package codegen

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"sync"
	"syscall"
	"time"

	"github.com/greatliontech/container"
	"github.com/greatliontech/ocifs"
)

type OCIFSRunner struct {
	ofs *ocifs.OCIFS

	// Default container state dir, can be overridden per Spec.StateDir.
	DefaultStateDir string
}

func NewOCIFSRunner(ofs *ocifs.OCIFS, defaultStateDir string) *OCIFSRunner {
	return &OCIFSRunner{
		ofs:             ofs,
		DefaultStateDir: defaultStateDir,
	}
}

func (r *OCIFSRunner) Start(ctx context.Context, spec Spec) (Process, error) {
	if spec.Image == "" {
		return nil, fmt.Errorf("runner: spec.Image is required")
	}

	// Mount image via OCIFS
	im, err := r.ofs.Mount(spec.Image)
	if err != nil {
		return nil, fmt.Errorf("runner: mount image: %w", err)
	}

	// Read OCI config to get default ENTRYPOINT/CMD, unless overridden
	conf := im.ConfigFile()

	entrypoint := conf.Config.Entrypoint
	cmd := conf.Config.Cmd

	// Resolve command line (with overrides)
	finalCmd, finalArgs, err := resolveCommand(entrypoint, cmd, spec.Command, spec.Args)
	if err != nil {
		_ = im.Unmount()
		_ = os.Remove(im.MountPoint())
		return nil, err
	}

	// Create a temp root target for the container's rootfs bind mount
	trgtroot, err := os.MkdirTemp(os.TempDir(), "ocifs-trgt-")
	if err != nil {
		_ = im.Unmount()
		_ = os.Remove(im.MountPoint())
		return nil, fmt.Errorf("runner: mk temp root: %w", err)
	}

	contID := "codegen-" + randHex(8)

	stateDir := spec.StateDir
	if stateDir == "" {
		stateDir = r.DefaultStateDir
	}
	if stateDir == "" {
		stateDir = defaultStateDir()
	}

	cfg := container.Config{
		Root:     trgtroot,
		Hostname: contID,
		Namespaces: container.Namespaces{
			NewIPC:  true,
			NewMnt:  true,
			NewNet:  true,
			NewPID:  true,
			NewUTS:  true,
			NewUser: true,
		},
		Mounts: []container.Mount{
			{
				Source: im.MountPoint(),
				Target: trgtroot,
				Type:   "auto",
				Flags:  syscall.MS_BIND | syscall.MS_RDONLY,
			},
		},
		UidMappings: []syscall.SysProcIDMap{
			{
				ContainerID: 0,
				HostID:      syscall.Getuid(),
				Size:        1,
			},
		},
		GidMappings: []syscall.SysProcIDMap{
			{
				ContainerID: 0,
				HostID:      syscall.Getgid(),
				Size:        1,
			},
		},
	}

	// NOTE: If your container runtime supports working dir/env directly on Process,
	// wire it here. Otherwise you'll need to wrap command in a tiny shell.
	pr := &container.Process{
		Cmd:        finalCmd,
		Args:       finalArgs,
		StdinPipe:  true,
		StdoutPipe: true,
		StderrPipe: true,
	}

	cont, err := container.New(stateDir, contID, cfg)
	if err != nil {
		cleanupLocal(im, trgtroot)
		return nil, fmt.Errorf("runner: create container: %w", err)
	}

	// Start process inside container
	if err := cont.Run(pr); err != nil {
		cleanupLocal(im, trgtroot)
		return nil, fmt.Errorf("runner: run: %w", err)
	}

	stdin, err := cont.StdinPipe()
	if err != nil {
		_ = bestEffortStop(cont)
		cleanupLocal(im, trgtroot)
		return nil, fmt.Errorf("runner: stdin pipe: %w", err)
	}
	stdout, err := cont.StdoutPipe()
	if err != nil {
		_ = stdin.Close()
		_ = bestEffortStop(cont)
		cleanupLocal(im, trgtroot)
		return nil, fmt.Errorf("runner: stdout pipe: %w", err)
	}
	stderr, err := cont.StderrPipe()
	if err != nil {
		_ = stdin.Close()
		_ = bestEffortStop(cont)
		cleanupLocal(im, trgtroot)
		return nil, fmt.Errorf("runner: stderr pipe: %w", err)
	}

	p := &ocifsProcess{
		ctx:      ctx,
		im:       im,
		trgtroot: trgtroot,
		cont:     cont,
		stdin:    stdin,
		stdout:   stdout,
		stderr:   stderr,
	}

	// Context cancellation: best-effort stop + close stdin to unblock.
	p.cancelOnce.Do(func() {}) // initialize
	go p.watchCancel()

	return p, nil
}

type ocifsProcess struct {
	ctx context.Context

	im       *ocifs.ImageMount
	trgtroot string
	cont     any // concrete type from container.New, but we only call known methods

	stdin  io.WriteCloser
	stdout io.Reader
	stderr io.Reader

	waitOnce   sync.Once
	waitStatus ExitStatus

	closeOnce  sync.Once
	cancelOnce sync.Once
}

func (p *ocifsProcess) Stdin() io.WriteCloser { return p.stdin }
func (p *ocifsProcess) Stdout() io.Reader     { return p.stdout }
func (p *ocifsProcess) Stderr() io.Reader     { return p.stderr }

func (p *ocifsProcess) Wait() ExitStatus {
	p.waitOnce.Do(func() {
		// container.Wait() in your snippet returns error only (no exit code).
		// We map nil -> 0, error -> 1. If you can expose exit code, plug it in here.
		type waiter interface{ Wait() error }
		w, ok := p.cont.(waiter)
		if !ok {
			p.waitStatus = ExitStatus{Code: 1, Err: fmt.Errorf("runner: container does not implement Wait() error")}
			return
		}
		err := w.Wait()
		if err == nil {
			p.waitStatus = ExitStatus{Code: 0, Err: nil}
			return
		}
		p.waitStatus = ExitStatus{Code: 1, Err: err}
	})
	return p.waitStatus
}

func (p *ocifsProcess) Close() error {
	var cerr error
	p.closeOnce.Do(func() {
		// Ensure stdin is closed; ignore error
		if p.stdin != nil {
			_ = p.stdin.Close()
		}
		_ = bestEffortStop(p.cont)
		cerr = cleanupLocal(p.im, p.trgtroot)
	})
	return cerr
}

func (p *ocifsProcess) watchCancel() {
	if p.ctx == nil {
		return
	}
	<-p.ctx.Done()
	p.cancelOnce.Do(func() {
		// Give the process a tiny grace period to finish naturally
		time.AfterFunc(150*time.Millisecond, func() {
			_ = bestEffortStop(p.cont)
		})
		if p.stdin != nil {
			_ = p.stdin.Close()
		}
	})
}

// ---------- helpers ----------

func resolveCommand(imageEntrypoint, imageCmd, overrideCommand, overrideArgs []string) (string, []string, error) {
	var ep []string
	var args []string

	if len(overrideCommand) > 0 {
		ep = overrideCommand
	} else {
		ep = imageEntrypoint
	}

	if len(overrideArgs) > 0 {
		args = overrideArgs
	} else {
		args = imageCmd
	}

	if len(ep) == 0 {
		return "", nil, fmt.Errorf("runner: no ENTRYPOINT/command available")
	}

	cmd0 := ep[0]
	finalArgs := []string{}
	if len(ep) > 1 {
		finalArgs = append(finalArgs, ep[1:]...)
	}
	if len(args) > 0 {
		finalArgs = append(finalArgs, args...)
	}
	return cmd0, finalArgs, nil
}

func cleanupLocal(im *ocifs.ImageMount, trgtroot string) error {
	var errs []error
	if im != nil {
		if err := im.Unmount(); err != nil {
			errs = append(errs, err)
		}
		// Your code removed mount point dir explicitly; keep doing it.
		if mp := im.MountPoint(); mp != "" {
			if err := os.Remove(mp); err != nil && !errors.Is(err, os.ErrNotExist) {
				errs = append(errs, err)
			}
		}
	}
	if trgtroot != "" {
		if err := os.RemoveAll(trgtroot); err != nil && !errors.Is(err, os.ErrNotExist) {
			errs = append(errs, err)
		}
	}
	return errors.Join(errs...)
}

// bestEffortStop attempts to stop/kill the container if the runtime exposes something.
// This is intentionally defensive because we don't know your container API surface.
func bestEffortStop(cont any) error {
	if cont == nil {
		return nil
	}

	// Prefer Kill() error
	type killer interface{ Kill() error }
	if k, ok := cont.(killer); ok {
		return k.Kill()
	}

	// Or Signal(sig) error
	type signaler interface{ Signal(os.Signal) error }
	if s, ok := cont.(signaler); ok {
		// Try SIGTERM first
		if err := s.Signal(syscall.SIGTERM); err == nil {
			return nil
		}
		return s.Signal(syscall.SIGKILL)
	}

	// Or Stop() error
	type stopper interface{ Stop() error }
	if st, ok := cont.(stopper); ok {
		return st.Stop()
	}

	return nil
}

func randHex(nBytes int) string {
	b := make([]byte, nBytes)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

func defaultStateDir() string {
	// keep it predictable but not necessarily /tmp/contstate
	d := os.TempDir() + string(os.PathSeparator) + "contstate"
	_ = os.MkdirAll(d, 0o755)
	return d
}

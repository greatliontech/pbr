package codegen

import (
	"context"
	"io"
)

type Spec struct {
	Image string

	// Optional. If empty, runner uses image default (ENTRYPOINT/CMD).
	Command []string // entrypoint override (k8s "command")
	Args    []string // cmd override (k8s "args")

	Env        map[string]string
	WorkingDir string

	// Optional hints
	StdinOnce bool // for k8s: stdinOnce

	// OCIFS options
	StateDir string // if empty, runner uses its DefaultStateDir
}

type Process interface {
	Stdin() io.WriteCloser
	Stdout() io.Reader
	Stderr() io.Reader

	Wait() ExitStatus
	Close() error // idempotent: cleanup pod/container/tempdirs
}

type ExitStatus struct {
	Code int
	Err  error // transport/runtime error (not process stderr)
}

type Runner interface {
	Start(ctx context.Context, spec Spec) (Process, error)
}

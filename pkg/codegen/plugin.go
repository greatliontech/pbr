package codegen

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os/exec"

	"github.com/containers/image/v5/copy"
	"github.com/containers/image/v5/manifest"
	"github.com/containers/image/v5/signature"
	"github.com/containers/image/v5/storage"
	"github.com/containers/image/v5/transports/alltransports"
	contstorage "github.com/containers/storage"
	"golang.org/x/sys/unix"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/pluginpb"
)

type Plugin struct {
	store contstorage.Store
	image string
}

func NewPlugin(store contstorage.Store, image string) *Plugin {
	return &Plugin{
		store: store,
		image: image,
	}
}

func (p *Plugin) Image() string {
	return p.image
}

func (p *Plugin) CodeGen(in *pluginpb.CodeGeneratorRequest) (*pluginpb.CodeGeneratorResponse, error) {
	// pull the image
	imgId, err := p.pullImage(p.image)
	if err != nil {
		return nil, err
	}

	// mount the image
	path, err := p.store.MountImage(imgId, nil, "")
	if err != nil {
		return nil, err
	}

	// get the image metadata
	imgMetaBytes, err := p.store.ImageBigData(imgId, "sha256:"+imgId)
	if err != nil {
		return nil, err
	}

	// parse the image metadata
	imgMeta := &manifest.Schema2Image{}
	if err := json.Unmarshal(imgMetaBytes, imgMeta); err != nil {
		return nil, err
	}

	// get the entrypoint
	entrypoint := imgMeta.Config.Entrypoint
	if len(entrypoint) == 0 {
		return nil, fmt.Errorf("no entrypoint found")
	}

	// prepare the plugin command
	cmd := exec.Command(entrypoint[0])
	cmd.SysProcAttr = &unix.SysProcAttr{
		Chroot:     path,
		Cloneflags: unix.CLONE_NEWNET,
	}

	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, err
	}

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, err
	}

	stderr, err := cmd.StderrPipe()
	if err != nil {
		return nil, err
	}

	if err := cmd.Start(); err != nil {
		return nil, err
	}

	// Marshal the input to protobuf binary format
	inData, err := proto.Marshal(in)
	if err != nil {
		return nil, err
	}

	// Write the marshaled data to stdin
	if _, err := stdin.Write(inData); err != nil {
		return nil, err
	}
	stdin.Close()

	// Read the output from stdout
	outData, err := io.ReadAll(stdout)
	if err != nil {
		return nil, err
	}

	// Also read from stderr
	errData, err := io.ReadAll(stderr)
	if err != nil {
		return nil, err
	}

	// Print anything that was sent to stderr
	if len(errData) > 0 {
		fmt.Printf("stderr: %s\n", string(errData))
		return nil, fmt.Errorf("stderr: %s", string(errData))
	}

	// Wait for the command to finish
	if err := cmd.Wait(); err != nil {
		return nil, err
	}

	if len(outData) == 0 {
		return nil, fmt.Errorf("no output data")
	}

	// Unmarshal the output into a CodeGeneratorResponse
	out := &pluginpb.CodeGeneratorResponse{}
	if err := proto.Unmarshal(outData, out); err != nil {
		return nil, err
	}

	return out, nil
}

func (p *Plugin) pullImage(imageName string) (string, error) {
	srcRef, err := alltransports.ParseImageName(imageName)
	if err != nil {
		fmt.Printf("Error parsing image name %v\n", imageName)
		return "", err
	}

	// systemCtx := &types.SystemContext{}
	// policy, err := signature.DefaultPolicy(systemCtx)
	// if err != nil {
	// 	fmt.Printf("Error getting default policy\n")
	// 	return err
	// }
	policy := &signature.Policy{
		Default: signature.PolicyRequirements{
			signature.NewPRInsecureAcceptAnything(),
		},
	}
	policyCtx, err := signature.NewPolicyContext(policy)
	if err != nil {
		return "", err
	}

	dstName := imageName
	if srcRef.DockerReference() != nil {
		dstName = srcRef.DockerReference().String()
	}
	dstRef, err := storage.Transport.ParseStoreReference(p.store, dstName)
	if err != nil {
		return "", err
	}

	copyOptions := &copy.Options{}
	manifestBytes, err := copy.Image(
		context.Background(),
		policyCtx,
		dstRef,
		srcRef,
		copyOptions,
	)
	if err != nil {
		return "", err
	}

	manifestMIMEType := manifest.GuessMIMEType(manifestBytes)
	manfst, err := manifest.FromBlob(manifestBytes, manifestMIMEType)
	if err != nil {
		return "", err
	}

	imgId, err := manfst.ImageID(nil)
	if err != nil {
		return "", err
	}

	return imgId, nil
}

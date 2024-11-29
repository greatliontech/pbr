package registry

import (
	"context"
	"encoding/hex"
	"fmt"

	modulev1alpha1 "buf.build/gen/go/bufbuild/buf/protocolbuffers/go/buf/alpha/module/v1alpha1"
	registryv1alpha1 "buf.build/gen/go/bufbuild/buf/protocolbuffers/go/buf/alpha/registry/v1alpha1"
	v1beta1 "buf.build/gen/go/bufbuild/registry/protocolbuffers/go/buf/registry/module/v1beta1"
	"connectrpc.com/connect"
)

func (reg *Registry) Download(ctx context.Context, req *connect.Request[v1beta1.DownloadRequest]) (*connect.Response[v1beta1.DownloadResponse], error) {
	fmt.Printf("Download: %v\n", req.Msg)

	resp := &connect.Response[v1beta1.DownloadResponse]{
		Msg: &v1beta1.DownloadResponse{},
	}

	for _, ref := range req.Msg.Values {
		refStr := ""
		var mod *internalModule
		var commit *v1beta1.Commit

		switch ref := ref.ResourceRef.Value.(type) {
		case *v1beta1.ResourceRef_Id:
			refStr = reg.commitHashes[ref.Id]
			mod = reg.commitToModule[ref.Id]
			commit = reg.commits[ref.Id]
		case *v1beta1.ResourceRef_Name_:
			return nil, fmt.Errorf("ResourceRef_Name_ not supported")
		}

		repo, err := reg.getRepository(ctx, mod.Owner, mod.Module)
		if err != nil {
			return nil, err
		}

		files, _, err := repo.FilesAndManifest(refStr)
		if err != nil {
			return nil, err
		}

		contents := &v1beta1.DownloadResponse_Content{
			Commit: commit,
		}

		for _, file := range files {
			if file.Name == "buf.yaml" {
				contents.V1BufLockFile = &v1beta1.File{
					Path:    file.Name,
					Content: []byte(file.Content),
				}
			}
			contents.Files = append(contents.Files, &v1beta1.File{
				Path:    file.Name,
				Content: []byte(file.Content),
			})
		}

		resp.Msg.Contents = append(resp.Msg.Contents, contents)
	}

	return resp, nil
}

func (reg *Registry) DownloadManifestAndBlobs(ctx context.Context, req *connect.Request[registryv1alpha1.DownloadManifestAndBlobsRequest]) (*connect.Response[registryv1alpha1.DownloadManifestAndBlobsResponse], error) {
	fmt.Printf("DownloadManifestAndBlobs: %v\n", req)

	resp := &connect.Response[registryv1alpha1.DownloadManifestAndBlobsResponse]{
		Msg: &registryv1alpha1.DownloadManifestAndBlobsResponse{},
	}

	repo, err := reg.getRepository(ctx, req.Msg.Owner, req.Msg.Repository)
	if err != nil {
		return nil, err
	}

	files, mani, err := repo.FilesAndManifest(req.Msg.Reference)
	if err != nil {
		return nil, err
	}

	maniDigest, err := hex.DecodeString(mani.SHAKE256)
	if err != nil {
		return nil, err
	}
	maniBlob := &modulev1alpha1.Blob{
		Digest: &modulev1alpha1.Digest{
			DigestType: modulev1alpha1.DigestType_DIGEST_TYPE_SHAKE256,
			Digest:     maniDigest,
		},
		Content: []byte(mani.Content),
	}

	for _, file := range files {
		fileDigest, err := hex.DecodeString(file.SHAKE256)
		if err != nil {
			return nil, err
		}
		fileBlob := &modulev1alpha1.Blob{
			Digest: &modulev1alpha1.Digest{
				DigestType: modulev1alpha1.DigestType_DIGEST_TYPE_SHAKE256,
				Digest:     fileDigest,
			},
			Content: []byte(file.Content),
		}
		resp.Msg.Blobs = append(resp.Msg.Blobs, fileBlob)
	}

	resp.Msg.Manifest = maniBlob

	return resp, nil
}

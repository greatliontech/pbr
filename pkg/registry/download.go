package registry

import (
	"context"
	"encoding/hex"
	"fmt"

	modulev1alpha1 "buf.build/gen/go/bufbuild/buf/protocolbuffers/go/buf/alpha/module/v1alpha1"
	registryv1alpha1 "buf.build/gen/go/bufbuild/buf/protocolbuffers/go/buf/alpha/registry/v1alpha1"
	"connectrpc.com/connect"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

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

func (reg *Registry) Download(_ context.Context, _ *connect.Request[registryv1alpha1.DownloadRequest]) (*connect.Response[registryv1alpha1.DownloadResponse], error) {
	return &connect.Response[registryv1alpha1.DownloadResponse]{}, status.Errorf(codes.Unimplemented, "not implemented")
}

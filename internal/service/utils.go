package service

import (
	"encoding/hex"

	v1beta1 "buf.build/gen/go/bufbuild/registry/protocolbuffers/go/buf/registry/module/v1beta1"
)

// getCommitObject creates a v1beta1.Commit object.
// Note: v1beta1 API uses B4 digest type for backwards compatibility.
func getCommitObject(ownerID, moduleID, commitID, filesDigestHex string) (*v1beta1.Commit, error) {
	digest, err := hex.DecodeString(filesDigestHex)
	if err != nil {
		return nil, err
	}
	return &v1beta1.Commit{
		Id:       commitID,
		OwnerId:  ownerID,
		ModuleId: moduleID,
		Digest: &v1beta1.Digest{
			Type:  v1beta1.DigestType_DIGEST_TYPE_B4,
			Value: digest,
		},
	}, nil
}

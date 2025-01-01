package registry

import (
	"encoding/hex"
	"fmt"

	v1beta1 "buf.build/gen/go/bufbuild/registry/protocolbuffers/go/buf/registry/module/v1beta1"
)

func getCommitObject(ownerId, modId, id, dgst string) (*v1beta1.Commit, error) {
	fmt.Println("getCommit")
	digest, err := hex.DecodeString(dgst)
	if err != nil {
		return nil, err
	}
	return &v1beta1.Commit{
		Id:       id,
		OwnerId:  ownerId,
		ModuleId: modId,
		Digest: &v1beta1.Digest{
			Type:  v1beta1.DigestType_DIGEST_TYPE_B4,
			Value: digest,
		},
	}, nil
}

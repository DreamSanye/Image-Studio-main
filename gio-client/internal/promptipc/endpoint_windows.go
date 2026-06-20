//go:build windows

package promptipc

import (
	"fmt"
	"hash/fnv"

	gioCompat "image-studio/gio-client/internal/compat"
)

func endpoint() (string, string, error) {
	root, err := gioCompat.StableDataRoot()
	if err != nil {
		return "", "", err
	}
	hash := fnv.New32a()
	_, _ = hash.Write([]byte(root))
	port := 42000 + int(hash.Sum32()%6000)
	return "tcp", fmt.Sprintf("127.0.0.1:%d", port), nil
}

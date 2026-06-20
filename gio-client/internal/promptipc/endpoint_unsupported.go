//go:build !windows && !linux

package promptipc

import "fmt"

func endpoint() (string, string, error) {
	return "", "", fmt.Errorf("prompt import control is not supported on this platform")
}

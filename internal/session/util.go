package session

import (
	"crypto/rand"
	"encoding/hex"
)

// newID 生成会话 id：8 字节随机数的十六进制，够短也够唯一。
func newID() string {
	b := make([]byte, 8)
	if _, err := rand.Read(b); err != nil {
		return hex.EncodeToString([]byte{0, 1, 2, 3, 4, 5, 6, 7})
	}
	return hex.EncodeToString(b)
}

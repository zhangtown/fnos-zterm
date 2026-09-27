// Package session 管理 xterm 会话：每个会话是一个 PTY 里的进程（本地 shell 或 ssh），
// 输出同时写进环形缓冲区，断线重连的客户端可以把最近的历史重新拉回来。
package session

// Ring 是定长的环形字节缓冲，保存终端最近的一段输出（滚动历史）。
// 它不是并发安全的：调用方（Session）负责加锁。
type Ring struct {
	buf  []byte
	size int
	w    int // 下一个写入位置
	n    int // 有效字节数
}

// NewRing 创建容量为 size 字节的环形缓冲；size <= 0 时取 256KiB。
func NewRing(size int) *Ring {
	if size <= 0 {
		size = 256 << 10
	}
	return &Ring{buf: make([]byte, size), size: size}
}

// Write 写入一段输出；超出容量的旧数据被覆盖。
func (r *Ring) Write(p []byte) {
	if len(p) >= r.size {
		p = p[len(p)-r.size:]
	}
	for len(p) > 0 {
		n := copy(r.buf[r.w:], p)
		r.w = (r.w + n) % r.size
		r.n += n
		if r.n > r.size {
			r.n = r.size
		}
		p = p[n:]
	}
}

// Snapshot 按时间顺序返回当前保存的全部字节（调用方拿到的是副本）。
func (r *Ring) Snapshot() []byte {
	out := make([]byte, r.n)
	start := (r.w - r.n + r.size) % r.size
	if start+r.n <= r.size {
		copy(out, r.buf[start:start+r.n])
	} else {
		c := copy(out, r.buf[start:])
		copy(out[c:], r.buf[:r.n-c])
	}
	return out
}

// Len 返回当前保存的字节数。
func (r *Ring) Len() int { return r.n }

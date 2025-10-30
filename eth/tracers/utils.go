package tracers

import (
	"errors"
	"fmt"

	"github.com/holiman/uint256"
)

// utils
const (
	memoryPadLimit = 1024 * 1024
)

func stackBack(st []uint256.Int, n int) *uint256.Int {
	return &st[len(st)-n-1]
}

func memoryCopy(m []byte, offset, size int64) (cpy []byte) {
	if size == 0 {
		return nil
	}

	if len(m) > int(offset) {
		cpy = make([]byte, size)
		copy(cpy, m[offset:offset+size])

		return
	}

	return
}

func memoryPtr(m []byte, offset, size int64) []byte {
	if size == 0 {
		return nil
	}

	if len(m) > int(offset) {
		return m[offset : offset+size]
	}

	return nil
}

func getMemoryCopyPadded(m []byte, offset, size int64) ([]byte, error) {
	if offset < 0 || size < 0 {
		return nil, errors.New("offset or size must not be negative")
	}
	length := int64(len(m))
	if offset+size < length { // slice fully inside memory
		return memoryCopy(m, offset, size), nil
	}
	paddingNeeded := offset + size - length
	if paddingNeeded > memoryPadLimit {
		return nil, fmt.Errorf("reached limit for padding memory slice: %d", paddingNeeded)
	}
	cpy := make([]byte, size)
	if overlap := length - offset; overlap > 0 {
		copy(cpy, memoryPtr(m, offset, overlap))
	}
	return cpy, nil
}

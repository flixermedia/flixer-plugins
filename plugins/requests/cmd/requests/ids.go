package main

import (
	"encoding/binary"
	"fmt"
	"math/rand/v2"
)

// newID returns a random version 4 UUID. Tests replace it to get stable ids.
// math/rand/v2 is seeded by the runtime from WASI random_get; ids are only
// identifiers (ownership is always checked), so crypto/rand is not needed and
// would add a large module memory footprint.
var newID = realNewID

func realNewID() string {
	var b [16]byte
	binary.BigEndian.PutUint64(b[0:8], rand.Uint64())
	binary.BigEndian.PutUint64(b[8:16], rand.Uint64())
	b[6] = b[6]&0x0f | 0x40
	b[8] = b[8]&0x3f | 0x80
	return fmt.Sprintf("%x-%x-%x-%x-%x", b[0:4], b[4:6], b[6:8], b[8:10], b[10:])
}

package main

import (
	"fmt"
	"testing"
)

func TestBasename(t *testing.T) {
	const filename = "/usr/local/test.flv"

	t.Log(fmt.Sprintf("%v-hevc.mp4", baseName(filename)))
}

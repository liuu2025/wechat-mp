package main

import (
	"fmt"
	"wechat-mp/internal/buildmeta"
)

func main() {
	fmt.Println(buildmeta.Detect(".").LinkerFlags())
}

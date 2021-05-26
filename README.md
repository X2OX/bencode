# [Bencode](https://github.com/x2ox/bencode)


[![JetBrains Open Source Licenses](https://img.shields.io/badge/-JetBrains%20Open%20Source%20License-000?style=flat-square&logo=JetBrains&logoColor=fff&labelColor=000)](https://www.jetbrains.com/?from=blackdatura)
[![GoDoc](https://pkg.go.dev/badge/go.x2ox.com/bencode.svg)](https://pkg.go.dev/go.x2ox.com/bencode)
[![Sourcegraph](https://sourcegraph.com/github.com/x2ox/bencode/-/badge.svg)](https://sourcegraph.com/github.com/x2ox/bencode?badge)
[![Go Report Card](https://goreportcard.com/badge/github.com/x2ox/bencode)](https://goreportcard.com/report/github.com/x2ox/bencode)
[![Release](https://img.shields.io/github/v/release/x2ox/bencode.svg)](https://github.com/x2ox/bencode/releases)
[![MIT license](https://img.shields.io/badge/license-MIT-brightgreen.svg)](https://opensource.org/licenses/MIT)

## Example
```go
package main

import (
	"fmt"
	
	"go.x2ox.com/bencode"
)

func main() {
	s1 := "this is a example"
	b, err := bencode.Marshal(&s1)
	if err != nil {
		return
	}
	fmt.Println(string(b))
	
	var s2 string
	if err = bencode.Unmarshal(b, &s2); err != nil {
		return
	}
	fmt.Println(s2)
}

```

## TODO
- Coverage test
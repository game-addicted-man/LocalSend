//go:build !windows

// LocalDrop 설치 프로그램은 Windows 전용이에요.
package main

import "fmt"

func main() {
	fmt.Println("LocalDrop 설치 프로그램은 Windows에서만 동작해요.")
}

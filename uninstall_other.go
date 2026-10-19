//go:build !windows

package main

import "fmt"

// runUninstall은 Windows에서만 의미가 있어요 (설치 프로그램이 Windows용이라서).
func runUninstall(quiet bool) {
	fmt.Println("제거는 Windows에서만 지원해요. 이 실행 파일과 설정 폴더를 지우면 돼요.")
}

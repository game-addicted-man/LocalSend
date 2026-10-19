module github.com/game-addicted-man/LocalSend

go 1.24.0

require (
	github.com/atotto/clipboard v0.1.4
	github.com/jchv/go-webview2 v0.0.0-20260205173254-56598839c808
	github.com/ncruces/zenity v0.10.14
	github.com/skip2/go-qrcode v0.0.0-20200617195104-da1b6568686e
	golang.org/x/sys v0.41.0
)

require (
	github.com/akavel/rsrc v0.10.2 // indirect
	github.com/dchest/jsmin v0.0.0-20220218165748-59f39799265f // indirect
	github.com/jchv/go-winloader v0.0.0-20250406163304-c1995be93bd1 // indirect
	github.com/josephspurrier/goversioninfo v1.4.1 // indirect
	github.com/randall77/makefat v0.0.0-20210315173500-7ddd0e42c844 // indirect
	golang.org/x/image v0.36.0 // indirect
)

// 초기화 실패 시 프로그램을 끄지 않도록 고친 사본 (third_party/go-webview2/LOCALDROP_PATCH.md)
replace github.com/jchv/go-webview2 => ./third_party/go-webview2

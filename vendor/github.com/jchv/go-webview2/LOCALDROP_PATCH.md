# LocalDrop에서 고친 부분

원본: https://github.com/jchv/go-webview2 (커밋 56598839c808, MIT 라이선스 — LICENSE 파일 참고)

WebView2 런타임이 있는데도 초기화가 실패하면(데이터 폴더 권한 문제, 손상된 런타임 등)
원본은 `log.Fatal`로 프로그램을 바로 끕니다. LocalDrop은 이때 브라우저로 화면을 열어야 하므로
아래만 바꿨습니다. 정상 동작 경로는 그대로입니다.

- `pkg/edge/chromium.go`: 환경·컨트롤러 생성 실패 시 `failed` 표시 후 `Embed()`가 `false`를 반환
- `webview.go`: `GetSettings` 실패 시 `nil` 반환, 설정 변경 실패는 로그만 남김

`cmd/` 예제 폴더는 뺐습니다.

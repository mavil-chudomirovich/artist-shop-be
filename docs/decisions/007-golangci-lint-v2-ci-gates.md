# ADR 007 - golangci-lint v2, action v9 và gate theo cỡ thay đổi

- **Status**: Accepted
- **Decision Date**: 2026-10-05
- **Decision Maker**: Mavil

## 1. Bối cảnh

CI fail với lỗi: `the Go language version (go1.24) used to build golangci-lint is
lower than the targeted Go version (1.26.0)`. Repo khai báo `go 1.26.0`. Workflow dùng
`golangci/golangci-lint-action@v6` với `version: latest`, và `latest` của dòng v1 là
`v1.64.8` — bản cuối dòng v1, build bằng go1.24. Khi thử pin `v2.14.0` lại fail tiếp:
`golangci-lint v2 is not supported by golangci-lint-action v6`.

## 2. Quyết định

1. Nâng lên **golangci-lint v2** và **action v9**, pin `version: v2.14.0` — đúng phiên
   bản CI dùng, để máy dev và CI cho cùng kết quả.
2. Migrate `.golangci.yml` sang schema v2: `version: "2"`, `linters.exclusions.rules`, và
   chuyển `gofmt`/`goimports` sang khối `formatters`.
3. Thêm `make lint` (fast gate) và `make check` (close-out gate), phân tầng theo cỡ
   thay đổi — xem bảng trong `AGENTS.md` §3.
4. `test-race` cần `CGO_ENABLED=1` và gcc: `make check` tự phát hiện và bỏ qua kèm
   thông báo thay vì báo đỏ.
5. Pin `golangci-lint` trong `make install-tools` để không lệch phiên bản.

Ngoài ra, thêm `.gitattributes` (`* text=auto eol=lf`) để mọi máy dùng LF, vì
`core.autocrlf=true` trên Windows khiến `gofmt -l .` báo hàng trăm file trong khi CI
(linux) vẫn xanh — tức là lint ở máy dev không đáng tin.

## 3. Hệ quả

| Hệ quả | Chi tiết |
|---|---|
| Tích cực | Lint ở máy khớp đúng kết quả CI |
| Tích cực | `make` là điểm vào duy nhất, Windows và Linux chạy lệnh giống nhau |
| Tiêu cực | Phải nâng cấp golangci-lint khi Go tăng phiên bản lớn |
| Tiêu cực | Cần hai lớp file compose và một target riêng cho việc kiểm tra đầy đủ |
| Bù đắp | Bảng gate theo cỡ thay đổi để không chạy `make check` (rất chậm) cho một thay đổi typo |

## 4. Cách tiếp cận bị loại

| Phương án | Vì sao không chọn |
|---|---|
| Hạ `go` trong `go.mod` xuống 1.24 | Hạ phiên bản ngôn ngữ của cả dự án chỉ để chiều lỏng công cụ |
| Bỏ golangci-lint, chỉ dùng `go vet` | Mất nhiều lỗi thật mà `go vet` không bắt (errcheck, staticcheck, revive) |
| Dùng `install-mode: goinstall` với `@latest` | Vẫn resolve ra v1.64.8; và build tool từ source chậm, không ổn định |

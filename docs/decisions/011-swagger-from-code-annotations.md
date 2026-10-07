# ADR 011 - Swagger sinh từ annotation trong code

- **Status**: Accepted
- **Decision Date**: 2026-10-07
- **Decision Maker**: Developer
- **Supersedes**: —
- **Superseded by**: —

## 1. Bối cảnh

Dự án đã có hai nguồn mô tả API: `docs/api-reference.md` (authoritative, viết tay,
xuyên module) và `specs/*/contracts/openapi.yaml` (artifact machine-readable sinh ở
bước `/speckit.plan`, chỉ cho feature đã có contract). Cả hai đều dễ lệch với code vì
không ai kiểm tra chúng trong CI, và không có giao diện để thử endpoint.

Dev muốn có `make swagger` để sinh spec machine-readable và một Swagger UI xem trực
tiếp. Có hai hướng: sinh từ annotation trong code (swaggo/swag), hoặc gộp các
`openapi.yaml` sẵn có.

## 2. Quyết định

- Sinh OpenAPI **từ annotation trong code** bằng `swaggo/swag`, pin version trong
  Makefile (`SWAG_VERSION ?= v1.16.4`). Lệnh: `make swagger` → `docs/swagger/`.
- Envelope dùng chung (`{data,meta}` / `{error}`) được mô tả bằng hai type export mới
  trong `internal/share/httpx/swagger.go`, tham chiếu qua cú pháp override của swag
  (`@Success 200 {object} httpx.SwaggerSuccess{data=...}`).
- Phục vụ Swagger UI ở `/swagger`, **tắt mặc định**, bật bằng `SWAGGER_ENABLED`. Route
  được composition root tiêm handler vào `httpserver.Dependencies.Swagger`; khi tắt,
  route không tồn tại.
- `make swagger-check` tái sinh spec và `git diff --exit-code docs/swagger`; CI chạy
  bước này để phát hiện lệch giữa annotation và file đã commit.
- `docs/api-reference.md` **vẫn là nguồn authoritative**. Khi generated spec và
  api-reference lệch nhau, api-reference thắng và generated spec phải được sửa cho
  khớp (Constitution VIII).

## 3. Hệ quả

Tích cực:

- Spec machine-readable phản ánh đúng route/handler trong code, có thể mở bằng tool
  ngoài hoặc Swagger UI.
- CI gate `swagger-check` biến việc quên cập nhật spec thành lỗi build, thay vì để
  lệch âm thầm như hai nguồn cũ.

Tiêu cực / cần lưu ý:

- Xuất hiện **nguồn spec thứ ba**; cần kỷ luật cập nhật và gate CI để không lệch.
  `specs/*/contracts/openapi.yaml` per-feature vẫn tồn tại song song.
- Annotation là comment, phải viết tay cho mỗi handler; thêm endpoint mới mà quên
  annotation thì endpoint vắng mặt trong spec (swag không báo lỗi).
- Thêm dependency runtime (`swaggo/http-swagger/v2`) và file generated phải commit vì
  `cmd/api` import package `docs/swagger`.

Điều kiện xem lại: nếu sau này chọn sinh spec trực tiếp từ contract (design-first)
thay vì code-first, viết ADR mới thay thế.

## 4. Cách tiếp cận bị loại

| Phương án | Vì sao không chọn |
|---|---|
| Gộp các `specs/*/contracts/openapi.yaml` | Chỉ bao phủ feature đã viết contract (thiếu module mới), và không phản ánh thay đổi route trong code |
| Chỉ validate `openapi.yaml`, không sinh từ code | Không có spec đầy đủ, không có UI thử endpoint |
| Sinh Swagger UI từ file tĩnh, không annotation | Spec không gắn với code, vẫn phải cập nhật tay ở nơi khác |

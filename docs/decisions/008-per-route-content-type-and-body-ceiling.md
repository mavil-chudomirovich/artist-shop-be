# ADR 008 - Kiểm tra content-type theo từng route, trần body toàn cục bằng hai lần trần avatar

- **Status**: Accepted
- **Decision Date**: 2026-10-06
- **Decision Maker**: Mavil
- **Feature**: `specs/003-user-profile`

## 1. Bối cảnh

Endpoint avatar là endpoint đầu tiên có body không phải JSON, và nó làm lộ ra hai giả
định mà pipeline dùng chung đang khẳng định cho **mọi** route:

- Pipeline áp `middleware.JSONContentType` cho cả mux. Middleware này trả `415` cho bất
  kỳ request nào có body không phải `application/json`, nên `multipart/form-data` không
  bao giờ tới được route upload: request bị từ chối trước khi route kịp chạy.
- Pipeline áp `middleware.BodyLimit(config.MaxBodyBytes)`, và middleware đó bọc body
  bằng `http.MaxBytesReader` **trước khi routing**
  (`internal/share/middleware/bodylimit.go`). Một trần riêng của route vì thế không thể
  nâng nó. Trần 2 MB mà FR-014 yêu cầu đơn giản là không tồn tại được dưới một trần
  toàn cục nhỏ hơn 2 MB, và giá trị mặc định lúc đó là 1 MiB
  (`specs/002-cross-cutting-foundation/quickstart.md`).

Nói cách khác, pipeline đang khẳng định hai điều mà nó không có quyền biết: "mọi route
đều nhận JSON" và "mọi route đều có cùng một trần body". Cả hai chỉ cần một module có
nhu cầu khác là sai. Constitution V yêu cầu upload phải được kiểm tra và whitelist (loại,
kích thước, số lượng); nếu pipeline từ chối request trước khi route chạy thì quy tắc
đó không bao giờ có dịp áp dụng.

## 2. Quyết định

Tách thành hai mức, mỗi mức chỉ khẳng định điều nó biết:

1. Pipeline giữ **một** khẳng định thô về body:
   `middleware.AllowedContentTypes(JSON, multipart)`
   (`internal/share/httpserver/routes.go`). Đây là danh sách mọi loại body mà dịch vụ
   này chấp nhận — nó nói "form-urlencoded, XML hay text thuần là sai ở mọi route" — chứ
   không nói route nào nhận loại nào.
2. Khẳng định "route này giải mã JSON" chuyển xuống **từng route**, dùng
   `middleware.JSONContentType`, trên đúng những route giải mã JSON
   (`internal/modules/user/presentation/http/router.go`: `PATCH /me`, `POST` và
   `PATCH /me/addresses`). Request không có body không bị ảnh hưởng.
3. `MAX_BODY_BYTES` mặc định lên **4 MiB** (4194304) — bằng **hai lần** trần 2 MB của
   avatar, chừa chỗ cho phần đệm `multipart`. Giá trị này vẫn là chặn sớm thô: pipeline
   chạy trước routing nên không route nào nâng được, và vì vậy nó phải lớn hơn **mọi**
   trần riêng của mọi route, không riêng trần của avatar.
4. Luật chính xác vẫn nằm ở route: `BodyLimit(2 MB + 64 KB)` trên route avatar, cộng
   một lần chặn nữa khi đọc part bằng `LimitReader` vì `Content-Length` không đáng tin.
   Pipeline không biết FR-014 là gì và không cần biết.

## 3. Hệ quả

| Hệ quả | Chi tiết |
|---|---|
| Tích cực | Một upload `multipart` thật sự tới được route xử lý; `internal/share/httpserver/routes_test.go` bảo vệ đúng điều đó bằng `TestAMultipartUploadReachesItsRoute` |
| Tích cực | Route giải mã JSON vẫn trả `415 UNSUPPORTED_MEDIA_TYPE` khi bị gửi loại body khác, kể cả `multipart` — kiểm tra đúng ở đúng chỗ, thay vì mất đi |
| Tích cực | Luật nghiệp vụ nằm ở route, đúng tầng theo Constitution I; tầng dùng chung chỉ còn khẳng định điều nó biết |
| Tích cực | Trần toàn cục vẫn chặn sớm một body quá lớn, trước khi nó bị đệm hết vào bộ nhớ và trước khi bất kỳ route nào chạy |
| Tiêu cực | Khẳng định JSON không còn miễn phí: một route `POST` mới quên gắn `JSONContentType` sẽ trả lỗi parse thay vì `415`. Sự quên này không lộ ra trong test của module — chỉ lộ ra khi client thật sự gửi sai, hoặc trong test pipeline |
| Tiêu cực | `MAX_BODY_BYTES` phải lớn hơn **mọi** trần riêng, nên nâng trần của một route có thể phải nâng cả hai con số. Quên thì một upload hợp lệ nhận `413` với lý do sai; cảnh báo đã ghi ở `docs/configuration.md` |
| Tiêu cực | Trần thô được nới lên 4 MiB: một endpoint khác giờ có thể đệm tới 4 MiB thay vì 1 MiB. Rate limit và các trần đọc còn lại vẫn là lớp bảo vệ thật |
| Bù đắp | Một loại body mới phải được khai trong pipeline đúng một chỗ, còn luật của từng route do route tự khai |

**Xem lại quyết định khi**: có loại body thứ ba mà service thực sự phải parse (ví dụ
`application/x-www-form-urlencoded`), hoặc khi một module cần trần lớn hơn trần toàn cục —
lúc đó hai con số phải nâng cùng nhau, và chính việc phải nâng cùng nhau đó là cái giá đã
chấp nhận ở trên.

## 4. Cách tiếp cận bị loại

| Phương án | Vì sao không chọn |
|---|---|
| Giữ khẳng định JSON toàn cục, chỉ khoét lỗ cho `multipart` | Cho phép upload tới route, nhưng pipeline hết khả năng phân biệt: một body `multipart` gửi tới route giải mã JSON sẽ lọt qua rồi chết ở tầng decode với lỗi parse — mất `415` và mất mã `UNSUPPORTED_MEDIA_TYPE`. Nó đổi một kiểm tra bị mất lấy một kiểm tra khác bị mất; muốn giữ cả hai thì phải có cả hai tầng |
| Chuyển `BodyLimit` xuống chạy sau routing | Middleware của chi chạy quanh handler đã khớp; để route tự đăng ký trần của nó thì pipeline phải tra một bảng trần theo route trước khi biết route nào sẽ chạy — tức đưa luật của module vào tầng dùng chung |
| Nâng `MAX_BODY_BYTES` rồi bỏ trần riêng của route | Biến một con số thô thành luật nghiệp vụ: đổi trần 2 MB của FR-014 sẽ phải sửa cấu hình dùng chung, và mọi endpoint khác cũng được nới theo một con số không liên quan tới chúng |
| Giữ trần toàn cục ở 1 MiB và cấm route nâng | Làm hằng số của tầng nền phụ thuộc FR của một module; module kế tiếp có body lớn hơn sẽ lại phải sửa tầng nền |
| Kiểm tra `Content-Type` bằng tay trong handler upload | Handler phải tự trả `415` trong khi pipeline vẫn khẳng định JSON ở mọi nơi: hai chỗ cùng nói về một luật, và chỗ nào sai thì người sau không biết phải sửa chỗ nào |

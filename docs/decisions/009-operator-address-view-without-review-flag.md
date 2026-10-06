# ADR 009 - Tra cứu của operator trả hình dạng địa chỉ riêng, không kế thừa cờ cần xem lại

- **Status**: Accepted
- **Decision Date**: 2026-10-06
- **Decision Maker**: Mavil
- **Feature**: `specs/003-user-profile`

## 1. Bối cảnh

Cùng một dòng địa chỉ được trả ra bởi hai đường khác nhau: danh sách của chính khách
(`GET /api/v1/users/me/addresses`) và tra cứu chỉ đọc của operator
(`GET /api/v1/users/{userId}`). Khác biệt không nằm ở dữ liệu — cả hai đọc cùng một
entity, cùng bản chụp tên đã lưu — mà nằm ở **thứ đường đó biết gì**:

- Danh sách của khách **biết** mã tỉnh/phường đã lưu còn hiệu lực hay không: use case tra
  từng mã với dataset chính thức qua port `Divisions` và gắn cờ `divisionNeedsReview`
  (`internal/modules/user/application/mapper/mapper.go`, research D10).
- Tra cứu của operator được dựng từ DTO hợp đồng liên module
  (`internal/contracts/user.go`, `CustomerAddress`), vốn không mang cờ này, và
  `Mapper.Customer` không tra dataset. Trên đường đó **không có bước nào** kiểm tra mã
  đã lưu với dataset.

Vậy nếu hai đường dùng chung một kiểu, cờ đó phải mang hai nghĩa khác nhau: một đường
biết và nói ra, một đường không biết và im lặng. Một `,omitempty` không thể phục vụ cả
hai mà nói thật.

## 2. Quyết định

Tách hai kiểu ở tầng presentation, mỗi kiểu trả lời đúng điều nó có:

- `AddressResponse` (`internal/modules/user/presentation/dto/dto.go`) **luôn** nói
  `divisionNeedsReview`, kể cả khi là `false`, vì đường của khách đã kiểm tra và biết
  câu trả lời.
- `OperatorAddressResponse` **không có** member đó, vì không có gì ở đường tra cứu đã
  kiểm tra. Vắng mặt nghĩa là "góc nhìn này không trả lời được", không phải "không cần
  xem lại".

Hợp đồng máy đọc được theo đúng hai hình dạng: `Address` và `CustomerLookupAddress`
trong `specs/003-user-profile/contracts/openapi.yaml`; `docs/api-reference.md` mô tả sự
khác biệt ở cả hai endpoint.

## 3. Hệ quả

| Hệ quả | Chi tiết |
|---|---|
| Tích cực | Không có `false` nào được khẳng định mà không có bước kiểm tra đứng sau nó |
| Tích cực | Client không phải đoán giữa "không cần xem lại" và "chưa ai kiểm": trong JavaScript vắng mặt là `undefined`, khác hẳn `false` |
| Tích cực | Edge case "dataset đổi sau khi đã lưu" được xử lý đúng chỗ: địa chỉ vẫn đọc được, vẫn trả tên đã chụp, và chỉ chủ của nó được báo cần remap |
| Tiêu cực | Hai kiểu phải giữ đồng nhất: một member mới trên địa chỉ phải thêm vào cả hai kiểu, cả hai schema OpenAPI và cả `docs/api-reference.md`. Không có gì ở tầng compile buộc làm việc đó |
| Tiêu cực | DTO hợp đồng không mang cờ, nên module Order (chưa tồn tại) muốn biết cờ thì phải gọi lại module user — thêm một lượt vào một luồng ghi đơn. Khi contract đã có người dùng, đổi DTO là quyết định phá vỡ (rule 5 của `docs/system-design/contract-purity.md`) |
| Bù đắp | Hai test khoá hành vi từng phía: một test khẳng định member này **vắng mặt** trong body của operator, một test khẳng định nó **có mặt** trong danh sách của khách kể cả khi là `false` |

Hệ quả phụ đã chấp nhận cùng cặp kiểu đó: hai danh sách còn khác nhau ở chỗ tài khoản
không có địa chỉ thì một bên trả `null`, bên kia trả `[]` — cùng nguyên tắc "trả đúng hình
dạng mà hợp đồng khai", đã ghi ở `docs/api-reference.md`.

## 4. Cách tiếp cận bị loại

| Phương án | Vì sao không chọn |
|---|---|
| Một kiểu chung với `json:"divisionNeedsReview,omitempty"` | `omitempty` làm mất **đúng** giá trị `false` — tức mất thứ mà danh sách của khách cần nói. Client đọc vắng mặt thành "không biết", và một client không có dataset thì không tự suy ra được |
| Một kiểu chung luôn nói cờ, tính cờ ở cả đường tra cứu | Đường tra cứu phải tra dataset để trả lời một câu không ai ở đó dùng, và cờ đó sẽ bị mọi consumer của hợp đồng kế thừa — kể cả consumer không có cách nào hành động theo. "Không có câu trả lời" trung thực hơn một câu trả lời không ai kiểm |
| Đưa cờ vào `CustomerAddress` và để consumer tự tra dataset | Dataset là nội bộ của module user, nhúng trong binary chứ không có bảng (ADR-002); consumer không được đọc nó. Đường này buộc module khác phải biết bản chất nội bộ của provider — vi phạm nguyên tắc 2 của `docs/system-design/contract-purity.md` |
| Bỏ cờ khỏi cả hai phía, để client tự lo | Client không có dataset. Edge case dataset đổi sau khi lưu sẽ trở thành một địa chỉ mà không ai biết cần remap |

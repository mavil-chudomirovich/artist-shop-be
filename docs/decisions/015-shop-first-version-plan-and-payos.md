# ADR 015 — Tái thứ tự roadmap theo version (Shop-first) và chọn PayOS

- **Status**: Accepted
- **Decision Date**: 2026-10-09
- **Decision Maker**: dev
- **Supersedes**: —
- **Superseded by**: —

## 1. Bối cảnh

Dự án chậm tiến độ và cần tung MVP sớm. Roadmap cũ trong `docs/modules.md` xếp trọn
"Giai đoạn 1 — Shop" trước "Giai đoạn 2 — Commission":
`04 → 05 → 06 → 07 → 08 → 09 → 16` rồi mới `10 → 11 → 12`.

Nền tảng đã xong tới hết `05 inventory`: danh mục, sản phẩm và kho đã sẵn sàng nhưng
**chưa bán được** vì thiếu giỏ/đơn/thanh toán. Trong khi `09 shipping` và `16 admin` là
phần "làm dày" trải nghiệm, không chặn việc bán hàng; còn `08 payment` là prerequisite
**chung** cho cả Shop lẫn Commission (`10` phụ thuộc `user` + `payment`).

Về thanh toán: `docs/product/backend-spec.md` (nguồn yêu cầu gốc) nêu MoMo/VietQR, nhưng
dev chọn **PayOS** — cổng thanh toán Việt Nam tạo payment link + QR và xác nhận qua webhook.

## 2. Quyết định

1. Đổi roadmap sang nhãn **version** (bỏ "Giai đoạn"):
   - **V0 — Nền tảng** (đã xong): `00 → 01 → 02 → 03`
   - **V1.0 — MVP bán hàng (Shop)**: `04 → 05` (đã xong) → **`06 cart → 07 order → 08 payment`**
   - **V1.1 — Commission**: **`10 commission → 11 chat`** (tái dùng `08`)
   - **V1.2 — Vận hành & phụ trợ**: `09 shipping → 12 notification → 16 admin`
   - **V2.0 — Mở rộng**: `13 review → 14 wishlist → 15 content`
2. Đảo thứ tự: `09`, `16`, `12` lùi về sau `10`, `11`.
3. **PayOS** là cổng thanh toán của module 08, thay cho MoMo/VietQR tự làm.
4. Cắt scope V1.0: phí ship **gộp trong Order** (chưa tách `09`); chưa có dashboard admin
   (`16`); chưa có thông báo (`12`).

## 3. Hệ quả

- V1.0 chỉ cần **3 module mới** và biến `04`/`05` đã xong thành doanh thu → MVP sớm nhất.
- `10`/`11` đứng ngay sau vì chỉ phụ thuộc `user` + `payment`.
- **Hệ thống không bị phá vỡ**: `00–05` giữ nguyên hành vi; chỉ **thêm** module mới và
  **mở lại** hợp đồng inventory đã hoãn (`specs/007-inventory-tracking/deferred.md` D1) cho
  Order/Payment gọi.
- **PayOS kéo theo yêu cầu vận hành mới**: tài khoản merchant + một endpoint webhook **công
  khai** (domain VPS; dev local cần tunnel) và verify chữ ký `checksumKey`; `orderCode` của
  PayOS là **số**, nên cần bảng map `order_code ↔ order_id`.
- `07` gộp phí ship rồi tách `09` sau có thể phải sửa data model đơn — chấp nhận, tách sớm
  ở V1.2.
- SDK PayOS chính thức hay tự viết bằng stdlib **chưa chốt ở đây**; sẽ quyết ở spec `08`
  (nếu thêm dependency thì ghi Complexity Tracking).
- Cập nhật `docs/modules.md` và `docs/modules/*.md` (đã làm cùng ADR này).

## 4. Cách tiếp cận bị loại

| Phương án | Vì sao không chọn |
|---|---|
| Giữ nguyên roadmap cũ (`09`, `16` trước `10`, `11`) | Đẩy commission — trụ cột bản sắc — ra quá xa; `09`/`16` không chặn việc bán |
| Commission-first (`08 → 10 → 11` trước Shop) | Sản phẩm + kho đã xây sẽ chưa bán được; `08` phải hỗ trợ commission ngay từ đầu (phức tạp hơn) |
| Giữ MoMo/VietQR tự làm như `backend-spec` | PayOS gói sẵn tạo QR + webhook + chữ ký, ít việc hơn và ít rủi ro bảo mật hơn tự viết |
| Giữ nhãn "Giai đoạn" cũ | Không diễn đạt được cam kết ra bản (version) và trộn trạng thái đã/chưa làm |

## 5. Bổ sung (2026-10-09)

Chốt các quyết định cho **V1.0** (`06 cart`, `07 order`, `08 payment`):

| # | Quyết định |
|---|---|
| D1 | Trạng thái đơn: `PENDING_PAYMENT → PAID → SHIPPED → COMPLETED`, cộng `CANCELLED`. Trạng thái thanh toán là chiều riêng, liên kết chặt với trạng thái đơn |
| D2 | **Đơn không có phí ship**: đơn chỉ gồm tiền hàng (subtotal). Khách **bắt buộc trả tiền hàng online** qua PayOS; **phí ship khách tự trả khi nhận hàng**; đơn vị vận chuyển là bên ngoài (ViettelPost). Vì vậy V1.0 **không** gọi API ViettelPost; module 09 (tracking) vẫn ở V1.2 |
| D3 | Giá trong giỏ: **snapshot lúc thêm** để hiển thị, nhưng **đối chiếu lại ở checkout**; nếu giá đã đổi thì từ chối và bắt khách xem lại giỏ |
| D4 | PayOS: **tự viết bằng stdlib** (HMAC + `net/http`), **không thêm dependency** vào `go.mod`. `orderCode` sinh bằng **số ngẫu nhiên int53 + thử lại khi trùng**, chốt bằng unique index |
| D5 | **Không xử lý hoàn tiền** ở V1.0. Chức năng huỷ **chỉ áp dụng cho đơn chưa thanh toán** (huỷ link PayOS + nhả giữ chỗ ở `05`) |

**Hệ quả trực tiếp**: Order không có cột phí ship, tổng tiền = tiền hàng; webhook PayOS xác nhận `PAID`; huỷ đơn chưa trả gọi PayOS cancel + nhả giữ chỗ inventory; không có luồng refund; không cần credential ViettelPost ở V1.0.

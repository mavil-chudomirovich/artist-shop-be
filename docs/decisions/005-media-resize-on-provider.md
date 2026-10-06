# ADR 005 - Thu nhỏ ảnh nhờ media provider, không thêm thư viện xử lý ảnh

- **Status**: Accepted
- **Decision Date**: 2026-10-05
- **Decision Maker**: Mavil
- **Feature**: `specs/003-user-profile`

## 1. Bối cảnh

Avatar phải có bề rộng tối đa 512 px trước khi lưu. Dự án hiện dùng Cloudinary làm
kho media, không có thư viện xử lý ảnh nào trong Go.

## 2. Quyết định

Tải **nguyên bản** lên media provider và yêu cầu provider thực hiện biến đổi kích
thước ngay trong lúc upload (target width 512). Không thêm dependency giải mã ảnh vào
dịch vụ.

Kiểm tra loại và kích thước vẫn thực hiện ở máy chủ trước khi upload, nên việc kiểm
tra không phụ thuộc vào provider.

## 3. Hệ quả

| Hệ quả | Chi tiết |
|---|---|
| Tích cực | Không thêm dependency lớn (kèm bề mặt CVE) vào binary |
| Tích cực | Build nhanh hơn, image nhỏ hơn, ít CPU hơn mỗi request |
| Tích cực | Định dạng đầu ra do provider quyết định, không phải do hai bên hiểu khác nhau |
| Tiêu cực | Phụ thuộc provider có hỗ trợ biến đổi khi upload |
| Tiêu cực | Nếu đổi provider, phải tìm được cơ chế tương đương |
| Bù đắp | Port `MediaStore` che giấu khác biệt, nên đổi provider chỉ đụng adapter |

## 4. Cách tiếp cận bị loại

| Phương án | Vì sao không chọn |
|---|---|
| Giải mã và resize trong Go | Dependency nặng, tốn CPU, và hai nơi có thể cho ra kết quả khác nhau |
| Để client resize trước khi upload | Máy chủ không thể tin kích thước do client khai; client có thể bỏ qua |

## 5. Bổ sung (2026-10-06)

Hai chi tiết dưới đây là **bổ sung** cho phần 2, không sửa quyết định gốc. Cả hai đều do
hiện thực mới buộc ra, và cả hai đều là chỗ một reviewer sẽ nhìn vào file khác và tự
hỏi: dòng `//nolint:gosec` trong adapter, và con số "512" trong hợp đồng.

### 5.1 Chữ ký yêu cầu là SHA-1 có khoá, không phải HMAC

Media provider quy định chữ ký một lệnh gọi như sau: gom mọi tham số **được ký** (loại
trừ chính payload, `cloud_name`, `resource_type`, `api_key` và `signature`), sắp xếp theo
tên tham số, nối các cặp `key=value` bằng `&`, **nối thêm API secret** vào cuối chuỗi
đó, rồi băm SHA-1 và trả về hex chữ thường. Hiện thực nằm ở
`internal/modules/user/infrastructure/implement/media/cloudinary.go` (`signedParameters`
và `sign`).

Đây là **hash có khoá, không phải HMAC** — và sự khác biệt không phải chuyện hình thức.
`hmac.New(sha1.New, secret)` là thuật toán khác, cho giá trị khác, và provider chỉ chấp
nhận đúng giá trị mà nó tự tính; dùng HMAC thì **mọi** lệnh gọi có chữ ký đều bị từ chối.

Vì vậy `crypto/sha1` được import kèm `//nolint:gosec` ngay tại dòng 5 của adapter, và
dòng đó phải được đọc đúng: gosec phản đối SHA-1 vì vai trò quen thuộc của nó là **hash
nội dung** — băm một file để so sánh, phát hiện va chạm. Ở đây SHA-1 là **thuật toán xác
thực do provider chỉ định**, dùng để chứng minh request do holder của secret gửi, và
không dùng ở đâu để băm hay kiểm tra nội dung. Ngoại lệ này thuộc về provider, không
phải lựa chọn của dịch vụ: bỏ nó đi thì `make lint` đỏ, đổi thuật toán thì tính năng
upload hỏng.

Chi phí được chấp nhận: SHA-1 bị xem là yếu, nên secret phải được coi là dữ liệu bí mật
nghiêm ngặt (chỉ nằm trong biến môi trường, không commit), và bất kỳ ai đọc dòng
`//nolint:gosec` đều phải hiểu rằng nó không phải chỗ dùng SHA-1 cho mục đích khác. Test
ở `internal/modules/user/infrastructure/implement/media/cloudinary_test.go` tự tính lại
chữ ký từ đúng tập tham số mà adapter gửi đi và so với giá trị adapter gửi, nên nó bảo
vệ hợp đồng với provider thay vì chỉ chép lại logic của chính nó.

### 5.2 `c_limit` là trần, không phải kích thước cố định

Biểu thức biến đổi mà adapter gửi lên là `c_limit,w_<n>,q_auto`. Trong đó `c_limit` là
resize mode **limit**: thu nhỏ khi ảnh **rộng hơn** target, còn ảnh **hẹp hơn** thì giữ
nguyên. Vì thế "width 512" trong hợp đồng phải được hiểu là **tối đa 512 px**, không
phải "đúng 512 px". Bỏ chữ `c_limit` thì một ảnh nhỏ sẽ bị kéo giãn lên 512 px — tức là
dịch vụ tự tạo ra ảnh mờ từ một ảnh khách vốn đã nhỏ và không thiếu gì.

Hai hệ quả cụ thể:

- `Avatar.width` trong `specs/003-user-profile/contracts/openapi.yaml` khai
  `maximum: 512` ("Always 512 at most") chứ không phải một hằng số bằng 512.
- Guard trần trong `internal/modules/user/domain/model/avatar.go` (`MaxAvatarWidth`,
  kiểm tra ở `NewAvatar`) gần như **không bao giờ nổ** trong vận hành bình thường. Nó
  chỉ bắt được bất thường: provider bỏ qua biến đổi, hoặc trả về chiều rộng lớn hơn
  trần. Đó là lưới an toàn của hạ tầng, không phải một luật kiểm mỗi request — đừng đọc
  nó như một kiểm tra thường xuyên. Giá trị vượt trần bị **từ chối**, không bị kẹp lại,
  vì kẹp sẽ lưu một kích thước mà ảnh không có.

Chiều cao không cùng hạn chế: `Avatar.height` không khai `maximum` và domain không chặn,
vì ảnh hẹp hơn 512 được giữ nguyên nên chiều cao do tỉ lệ gốc quyết định. Hệ quả client
phải chấp nhận: kích thước ảnh lưu **không đồng nhất**, và không được giả định
`width == 512`.

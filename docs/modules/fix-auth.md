# Kế hoạch chỉnh sửa luồng đăng ký và xác thực OTP cho GreenWheel

Mục tiêu là sửa lại luồng đăng ký tài khoản để chỉ lưu tài khoản chính thức vào SQL Server sau khi người dùng xác thực OTP thành công, đồng thời xử lý tốt trường hợp người dùng thoát giữa chừng, OTP hết hạn, gửi lại OTP hoặc hệ thống gặp lỗi.

Mình đề xuất dùng Redis để lưu trạng thái đăng ký tạm thời, SQL Server để lưu tài khoản chính thức. Khi người dùng thoát khỏi màn hình OTP, hệ thống không xóa dữ liệu ngay mà để Redis tự hết hạn theo TTL.

## 1. Thiết kế luồng tổng thể

Bước 1 — Người dùng đăng ký

Nhập email, mật khẩu và thông tin cá nhân

Bước 2 — Kiểm tra dữ liệu

Validate input, kiểm tra email, username trong DB và giới hạn số lần yêu cầu OTP

Bước 3 — Lưu đăng ký tạm vào Redis

Lưu password hash, thông tin cần thiết và trạng thái PendingVerification; tạo OTP và gửi email

Bước 4 — Người dùng nhập OTP

Kiểm tra OTP, thời hạn và số lần nhập sai

OTP hợp lệ

Tạo tài khoản trong DB

Kiểm tra email duy nhất, lưu tài khoản

Xóa dữ liệu Redis tạm sau khi DB xác nhận thành công

OTP không hợp lệ

Giữ trạng thái tạm

Tăng số lần sai, trả lỗi phù hợp

Cho nhập lại hoặc gửi OTP mới nếu đủ điều kiện

Quy tắc quan trọng: Không xóa dữ liệu đăng ký khỏi Redis chỉ vì người dùng đóng trình duyệt hoặc rời màn hình OTP. Dữ liệu sẽ tự hết hạn theo TTL. Chỉ xóa sau khi đăng ký thành công, người dùng chủ động hủy, hoặc theo chính sách dọn dẹp đã thiết kế.

## 2. Các quyết định cần chốt trước khi sửa code

| Hạng mục                        | Phương án đề xuất                 |
| ------------------------------- | --------------------------------- |
| Nơi lưu tài khoản chưa xác thực | Redis                             |
| Nơi lưu tài khoản chính thức    | SQL Server                        |
| Thời gian sống của OTP          | 5 phút                            |
| Thời gian sống của đăng ký tạm  | 15–30 phút                        |
| Thời gian chờ gửi lại OTP       | 60 giây                           |
| Số lần nhập OTP sai             | Tối đa 5 lần cho mỗi mã           |
| Người dùng thoát màn hình OTP   | Giữ dữ liệu tạm đến khi hết TTL   |
| Người dùng muốn hủy đăng ký     | Cung cấp API hủy; xóa dữ liệu tạm |
| OTP hợp lệ nhưng DB lỗi         | Giữ dữ liệu cần thiết để thử lại  |
| Email đã có tài khoản           | Không tạo đăng ký mới             |
| Gửi OTP mới                     | Mã mới vô hiệu hóa mã cũ          |

Các con số trên là giá trị khởi đầu để triển khai, không phải quy tắc bắt buộc. Cần điều chỉnh theo yêu cầu bảo mật và trải nghiệm của GreenWheel.

## 3. Kế hoạch triển khai theo từng giai đoạn

## Giai đoạn 1 — Rà soát luồng hiện tại

Ưu tiên 1

Kiểm tra code trước khi thay đổi để tránh phá vỡ những luồng xác thực đang hoạt động.

- Tìm API đăng ký, xác thực OTP và gửi lại OTP.
- Tìm service gửi email OTP và nơi tạo OTP.
- Kiểm tra cách lưu User vào SQL Server hiện tại.
- Kiểm tra cách dùng Redis, cấu trúc key và TTL.
- Kiểm tra luồng đăng nhập, Google login và các luồng xác thực liên quan.
- Xác định trường hợp nào đang tạo User vào DB trước khi xác thực OTP.

Đầu ra: Sơ đồ luồng hiện tại và danh sách service, repository, DTO cần chỉnh sửa.

## Giai đoạn 2 — Thiết kế dữ liệu đăng ký tạm

Ưu tiên 2

Thiết kế dữ liệu Redis độc lập với entity User chính thức.

- Tạo model `PendingRegistration`.
- Lưu `RegistrationId`, email đã chuẩn hóa, password hash và các trường đăng ký cần thiết.
- Lưu trạng thái, thời điểm hết hạn và thông tin cần thiết để retry.
- Lưu OTP dưới dạng hash thay vì plaintext.
- Thiết lập TTL cho dữ liệu đăng ký và OTP.
- Thiết kế quy tắc cập nhật hoặc thay thế OTP khi gửi lại.
- Đảm bảo không lưu password plaintext trong Redis hoặc log.

Đầu ra: Model, quy ước Redis key, TTL và service thao tác dữ liệu tạm.

## Giai đoạn 3 — Sửa API đăng ký

Ưu tiên 3

Khi người dùng gửi thông tin đăng ký:

- Validate dữ liệu bằng cơ chế hiện có, chẳng hạn FluentValidation.
- Chuẩn hóa email trước khi kiểm tra trùng.
- Kiểm tra email đã tồn tại trong SQL Server.
- Kiểm tra giới hạn đăng ký và gửi OTP theo email/IP.
- Tạo `RegistrationId` đủ ngẫu nhiên, khó đoán.
- Hash mật khẩu bằng cơ chế password hashing của hệ thống.
- Lưu dữ liệu đăng ký tạm vào Redis.
- Tạo OTP, lưu hash OTP và gửi email.
- Trả về `RegistrationId`, thời hạn OTP và thời gian có thể gửi lại.

Điều kiện hoàn thành: Gọi API đăng ký chưa tạo User chính thức trong DB.

Lưu ý: Nếu gửi email thất bại, cần xử lý dữ liệu tạm nhất quán, cho phép thử lại có kiểm soát hoặc xóa bản ghi tạm theo chính sách rõ ràng.

## Giai đoạn 4 — Sửa API xác thực OTP

Ưu tiên 4

Đây là phần quan trọng nhất của thay đổi.

- Nhận `RegistrationId` và OTP từ client.
- Tìm dữ liệu đăng ký tạm trong Redis.
- Kiểm tra dữ liệu đăng ký và OTP còn hạn.
- Kiểm tra số lần nhập sai.
- So sánh OTP với hash đã lưu theo cách an toàn.
- OTP sai: tăng số lần thử, trả lỗi phù hợp và giữ dữ liệu tạm nếu chưa vượt giới hạn.
- OTP hết hạn: yêu cầu gửi lại OTP, không tạo tài khoản.
- OTP đúng: tiến hành tạo User trong SQL Server.
- Chỉ xóa dữ liệu đăng ký tạm sau khi việc tạo User được xác nhận thành công.

Điều kiện hoàn thành: Không thể tạo tài khoản bằng OTP sai, hết hạn hoặc không tồn tại.

## Giai đoạn 5 — Gửi lại OTP, hủy đăng ký và khôi phục

Ưu tiên 5

- Tạo hoặc sửa API `resend-otp`.
- Không cho gửi lại trước khi hết cooldown.
- OTP mới thay thế OTP cũ và có thời hạn mới.
- Giới hạn số lần gửi theo đăng ký, email và IP.
- Tạo API hủy đăng ký nếu sản phẩm cần chức năng này.
- Không xóa dữ liệu khi người dùng chỉ đóng trang hoặc rời màn hình.
- Nếu SQL Server lỗi tạm thời, giữ trạng thái đủ để retry.
- Xử lý trường hợp DB đã tạo User nhưng client không nhận được response, tránh tạo trùng tài khoản.

Điều kiện hoàn thành: Các tình huống gián đoạn không làm mất dữ liệu hoặc tạo tài khoản trùng.

## Giai đoạn 6 — Kiểm thử và triển khai

Ưu tiên 6

- Unit test service đăng ký và xác thực OTP.
- Test OTP đúng, sai, hết hạn và vượt số lần thử.
- Test cooldown và gửi lại OTP.
- Test Redis hết hạn hoặc không khả dụng.
- Test SQL Server lỗi trong lúc tạo User.
- Test hai request xác thực đồng thời.
- Test email đã tồn tại và đăng ký trùng.
- Test đăng nhập bình thường sau khi đăng ký thành công.
- Kiểm tra log không chứa password hoặc OTP.
- Kiểm thử tích hợp trên môi trường staging trước khi triển khai production.

Điều kiện hoàn thành: Đăng ký mới, xác thực OTP, đăng nhập và các luồng auth liên quan đều hoạt động.

## 4. Thiết kế API sau khi chỉnh sửa

Bạn có thể giữ API hiện tại nếu đã có cấu trúc phù hợp; bảng dưới đây là thiết kế mục tiêu để đối chiếu, không bắt buộc phải đổi toàn bộ tên endpoint.

| Method | Endpoint                        | Trách nhiệm                                              |
| ------ | ------------------------------- | -------------------------------------------------------- |
| `POST` | `/api/auth/register`            | Validate thông tin, tạo đăng ký tạm trong Redis, gửi OTP |
| `POST` | `/api/auth/register/verify-otp` | Xác thực OTP và tạo User trong DB                        |
| `POST` | `/api/auth/register/resend-otp` | Gửi OTP mới khi đủ điều kiện                             |
| `POST` | `/api/auth/register/cancel`     | Hủy đăng ký tạm theo yêu cầu người dùng                  |

### Ví dụ response khi đăng ký thành công bước đầu

```

{
  "success": true,
  "message": "Registration initiated. Please verify your email.",
  "data": {
    "registrationId": "random-registration-id",
    "status": "PendingVerification",
    "otpExpiresIn": 300,
    "resendAvailableIn": 60
  }
}

```

`registrationId` cần được tạo bằng bộ sinh số ngẫu nhiên an toàn. Client dùng ID này để xác thực và gửi lại OTP, thay vì dựa vào một email do client tự gửi ở mỗi request. Không trả OTP về response trong môi trường production.

## 5. Phân chia trách nhiệm trong Clean Architecture

Với kiến trúc ASP.NET Core hiện tại của GreenWheel, mình đề xuất phân chia như sau:

| Layer          | Công việc                                                                   |
| -------------- | --------------------------------------------------------------------------- |
| API            | Nhận request, validate input, gọi Application và trả response               |
| Application    | Điều phối đăng ký, xác thực OTP, gửi lại OTP và xử lý trạng thái            |
| Domain         | Chứa các quy tắc nghiệp vụ thực sự cần thiết; không phụ thuộc Redis         |
| Infrastructure | Triển khai Redis, repository SQL Server, gửi email và các dịch vụ liên quan |

Các thành phần có thể cần tạo hoặc chỉnh sửa:

- `RegisterCommand` / `RegisterRequest`
- `VerifyRegisterOtpCommand` / `VerifyRegisterOtpRequest`
- `ResendRegisterOtpCommand`
- `CancelRegistrationCommand`
- `PendingRegistration`
- `IRegistrationCacheService` hoặc interface tương đương
- `RegistrationCacheService`
- `IEmailService` hoặc dịch vụ gửi OTP đang có
- `AuthService` / handler đăng ký và xác thực OTP hiện tại

Đây là danh sách gợi ý. Nên tận dụng service, DTO và interface đang có để tránh tạo nhiều lớp trùng trách nhiệm.

## 6. Những lỗi nghiệp vụ phải xử lý

| Tình huống                             | Kết quả mong muốn                     |
| -------------------------------------- | ------------------------------------- |
| Người dùng đóng trang OTP              | Dữ liệu tạm vẫn còn đến khi hết TTL   |
| OTP sai                                | Không tạo User, tăng số lần thử       |
| OTP hết hạn                            | Không tạo User, yêu cầu gửi lại       |
| Gửi lại OTP quá sớm                    | Từ chối và trả thời gian còn phải chờ |
| Email đã tồn tại                       | Không tạo đăng ký trùng               |
| Redis hết hạn dữ liệu                  | Yêu cầu bắt đầu lại quy trình đăng ký |
| SQL Server lỗi khi xác thực thành công | Không xóa dữ liệu cần thiết để retry  |
| Hai request xác thực cùng lúc          | Không tạo hai User                    |
| DB đã lưu nhưng response bị mất        | Request retry không tạo User trùng    |

Điểm cần đặc biệt chú ý: Redis và SQL Server không dùng chung một transaction thông thường. Vì vậy, không nên xóa dữ liệu Redis ngay sau khi OTP hợp lệ nhưng trước khi DB xác nhận lưu thành công. Đồng thời, cần có unique constraint cho email trong DB và xử lý idempotency hoặc kiểm tra tài khoản đã được tạo khi retry.

## 7. Checklist nghiệm thu

Bạn có thể dùng checklist này để theo dõi tiến độ sửa luồng:

## Tiến độ thực hiện

### 0/18

Luồng chính

Đăng ký chỉ tạo dữ liệu tạm trong Redis

OTP đúng mới tạo User trong SQL Server

Đăng ký thành công thì xóa dữ liệu tạm

Đăng nhập được sau khi xác thực thành công

OTP và Redis

OTP có TTL

Đăng ký tạm có TTL riêng

OTP mới vô hiệu hóa OTP cũ

Giới hạn nhập sai và gửi lại OTP

Thoát màn hình không tự xóa dữ liệu

Tính nhất quán và bảo mật

Không lưu password plaintext

Không log OTP hoặc password

Email có unique constraint

Retry không tạo User trùng

Xử lý lỗi DB và request đồng thời

Kiểm thử

Unit test service

Integration test Redis và DB

Test các trường hợp lỗi OTP

Test đăng nhập và các luồng auth hiện có

Đặt lại checklistSao chép checklist

## 8. Thứ tự sửa code mình khuyên bạn làm

Đừng sửa tất cả cùng lúc. Hãy làm theo thứ tự sau:

1. Đọc luồng đăng ký hiện tại và xác định chỗ đang lưu User vào DB.
2. Thiết kế model và Redis key cho `PendingRegistration`.
3. Sửa API đăng ký để chỉ lưu dữ liệu tạm.
4. Sửa API verify OTP để tạo User sau khi xác thực thành công.
5. Sửa resend OTP, TTL, giới hạn số lần thử.
6. Xử lý retry, lỗi DB và request đồng thời.
7. Viết test rồi mới triển khai.

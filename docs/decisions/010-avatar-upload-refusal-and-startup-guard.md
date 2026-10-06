# ADR 010 - Route avatar báo lý do của nó, và từ chối khởi động khi trần toàn cục thấp hơn trần của route

- **Status**: Accepted
- **Decision Date**: 2026-10-06
- **Decision Maker**: Mavil
- **Feature**: `specs/004-fix-pending-defects`
- **Bổ sung**: [ADR 008](008-per-route-content-type-and-body-ceiling.md) — mở rộng phần
  "trần toàn cục phải lớn hơn mọi trần riêng" mà ADR 008 mới chỉ mới **ghi nhận là cái
  giá chấp nhận được**

## 1. Bối cảnh

ADR 008 tách trần body thành hai mức: `MAX_BODY_BYTES` là chặn sớm thô của cả pipeline,
còn route giữ luật chính xác của mình. Nó cũng ghi thẳng cái giá của việc đó: `MAX_BODY_BYTES`
phải lớn hơn **mọi** trần riêng, nên nâng trần của một route có thể phải nâng cả hai con
số, và **quên thì một upload hợp lệ nhận `413` với lý do sai**. ADR 008 chấp nhận cái giá
đó dưới dạng rủi ro đã ghi.

Feature `004-fix-pending-defects` phát hiện rủi ro đó đã xảy ra thật, theo hai kiểu khác
nhau, và cả hai đều do **tầng dùng chung không biết mình đang nói về cái gì**:

**Một — cùng một lỗi, hai mã lỗi.** `middleware.BodyLimit` bọc body bằng
`http.MaxBytesReader`, và một client khai `Content-Length` thì bị chặn ngay ở đó, trước
khi route avatar kịp chạy. Tầng pipeline chỉ có một câu trả lời duy nhất cho "body quá
lớn", và câu đó là `PAYLOAD_TOO_LARGE`. Route avatar có câu trả lời riêng,
`USER_AVATAR_TOO_LARGE`, nhưng chỉ tới được khi client **không** khai độ dài. Kết quả:

| Client gửi avatar quá trần | Trước |
|---|---|
| Khai `Content-Length` | `413 PAYLOAD_TOO_LARGE` |
| Không khai (chunked) | `413 USER_AVATAR_TOO_LARGE` |
| Khai thiếu | `413 USER_AVATAR_TOO_LARGE` |

Khách chỉ cần một câu trả lời duy nhất, và họ không thể đoán trước sẽ nhận mã nào: điều
duy nhất quyết định là client có tự khai kích thước hay không, một chi tiết client không
kiểm soát ở chỗ nó quan trọng nhất. Tệ hơn, `PAYLOAD_TOO_LARGE` trở nên **không đúng ở
mọi nơi khác** nó xuất hiện: nó vẫn đúng cho một request JSON quá lớn, và không còn đúng
cho ảnh quá lớn.

**Hai — cấu hình sai không ai nói với operator.** Nếu `MAX_BODY_BYTES` thấp hơn trần route
avatar, thì **mọi** upload avatar bị pipeline chặn trước khi route chạy, với
`PAYLOAD_TOO_LARGE` — một mã không chỉ ra biến nào cần sửa. Mối ràng buộc giữa hai con số
tồn tại chỉ như một dòng cảnh báo trong tài liệu và một dòng bình luận trong code. Đó là
một ràng buộc không ai canh giữ: sớm hay muộn nó sẽ vỡ, và sẽ vỡ trên máy của khách chứ
không phải trên máy của người viết.

Hai vấn đề này có chung một nguyên nhân: **một tầng không biết mình đang nói về cái gì
thì vẫn phải phát ra một câu trả lời**, và câu trả lời đó thành luật.

## 2. Quyết định

### 2.1 Route avatar báo lý do của chính nó

`middleware.BodyLimit` nhận thêm một tham số: **mã lỗi mà lần từ chối đó sẽ báo**.
Không truyền thì nó dùng `PAYLOAD_TOO_LARGE` như trước, nên **không caller nào đang có
đổi hành vi**. Route avatar truyền `USER_AVATAR_TOO_LARGE` — đúng mã mà handler của nó
đã trả cho hai dạng còn lại.

Luật trở lại thành: `PAYLOAD_TOO_LARGE` nghĩa đúng như câu chữ của nó, *mọi route không
có luật riêng về nội dung*. Route nào có luật riêng thì trả mã của luật đó, vì mã đó mới
chỉ cho client biết **chính xác cần sửa gì**.

### 2.2 Từ chối khởi động thay vì ghi log cảnh báo

`RequireAvatarUploadCeiling(sharedCeiling, avatarCeiling, mediaConfigured)` là một hàm
**thuần**, trả `error`, nằm cạnh route avatar và **đọc con số từ chính hàm mà route dùng**
(`AvatarUploadCeiling`). Composition root gọi nó **trước khi** mở database, chạy migration
hay mở cổng.

Ba điều kiện cố ý:

| Điều kiện | Kết quả |
|---|---|
| Có media **và** trần toàn cục **thấp hơn** trần route | Từ chối khởi động, exit code khác 0 |
| Có media **và** trần toàn cục **bằng** trần route | Khởi động |
| **Không** có media, bất kỳ giá trị nào | Khởi động, kèm một dòng cảnh báo nêu hệ quả |

**Biên "bằng vẫn chạy" là cố ý.** Ở mức bằng, trần của route là ràng buộc thật sự, và nó
đang chạy đúng như thiết kế. Từ chối một cấu hình **đang hoạt động** vì lý do thuần kỹ
thuật là điều operator sẽ phải vượt qua để đưa hệ thống lên, và sẽ phải vượt qua mỗi lần
deploy. Ranh giới được ghim ở cả hai chiều bằng test.

**Không có media thì không từ chối.** Một triển khai không có media không thể upload
được, nên trần này không liên quan tới nó; chặn nó bằng một ràng buộc chỉ áp cho upload là
sai. Đổi lại, composition ghi một dòng cảnh báo nêu **tên biến** chứ không nêu giá trị:
thiếu cấu hình media thì upload avatar trả `USER_MEDIA_UNAVAILABLE`, và
`MAX_BODY_BYTES` không được kiểm tra với trần avatar.

**Con số có một chủ duy nhất.** Hàm guard không chứa literal; nó nhận con số từ
`AvatarUploadCeiling`, cùng hàm mà route đọc. Một literal sao chép vào tầng dùng chung
sẽ là con số thứ hai phải giữ đồng bộ — đúng thứ Constitution I cấm khi tầng dùng chung
phải import một module.

## 3. Hệ quả

| Hệ quả | Chi tiết |
|---|---|
| Tích cực | Một ảnh quá lớn có đúng một câu trả lời, không phụ thuộc client có khai `Content-Length` hay không. Đã kiểm chứng ở cả ba dạng gửi |
| Tích cực | `PAYLOAD_TOO_LARGE` lại đúng ở mọi nơi nó xuất hiện: một mã, một nghĩa |
| Tích cực | Một cấu hình sai bị chặn lúc khởi động, trước database, trước migration, trước cổng — thay vì là một `413` khó hiểu lúc 3 giờ sáng |
| Tích cực | Thông điệp từ chối nêu tên biến và **cả hai** giá trị, nên operator sửa được mà không cần đọc source hay tài liệu. Nó chỉ nhận hai con số byte và một boolean, nên không có gì để lộ credential |
| Tích cực | Guard là hàm thuần nên mọi nhánh được unit test **không cần database** — tránh được cái bẫy là test trong `cmd/api` sẽ phải mở hạ tầng thật |
| Tích cực | Không có media vẫn khởi động được, nên yêu cầu "migrate/seed chạy được không cần credential media" không bị phá vỡ |
| Tiêu cực | **Một deployment thêm biến môi trường phải qua một gate mới.** Thêm một route có trần lớn hơn trần toàn cục thì phải nâng cả hai con số, và giờ việc quên đó **làm API không khởi động**. Đây là cái giá lớn nhất, và nó được chọn có ý thức: đổi lại cái quên đó bị phát hiện ngay lần deploy đầu tiên, không phải bởi một khách hàng |
| Tiêu cực | Biên "thấp hơn" là con số tuyệt đối lấy từ module khác. Một module sau này có trần riêng lớn hơn sẽ phải thêm một phép so sánh nữa ở composition root, và composition root sẽ dần thành nơi tập hợp các trần của từng module |
| Tiêu cực | `BodyLimit` giờ có hai đường: bản một tham số và bản hai tham số. Caller mới có thể chọn nhầm; đường không có tham số vẫn giữ nguyên nên độ lỗi là dùng nhầm ở route mới chứ không phải hỏng route cũ |
| Tiêu cực | Message từ chối nói thẳng "raise MAX_BODY_BYTES", tức là nó **khuyến nghị** một thay đổi chỉ nới hạn mức toàn cục. Đúng về mặt kỹ thuật, nhưng nó không phải lời khuyên duy nhất trong mọi tình huống — nếu `MAX_BODY_BYTES` cố ý thấp vì lý do riêng thì cách đúng là cân nhắc bỏ cấu hình media |
| Bù đắp | Việc kiểm tra chạy ở composition root, tức nơi duy nhất thấy cả hai con số. Đổi lại, không có lớp dùng chung nào "tự bảo vệ": một caller mới của `BodyLimit` vẫn phải tự biết trần của nó có lớn hơn trần toàn cục hay không |

**Xem lại quyết định khi**: có module thứ hai cần trần body riêng lớn hơn trần toàn cục —
lúc đó một phép so sánh nữa ở composition root là chấp nhận được, nhưng nếu con số thứ ba
xuất hiện thì cơ chế "so sánh ở composition root" đã hết cửa và cần thay bằng một cách
khác (ví dụ trần toàn cục tính từ trần lớn nhất đã khai).

## 4. Cách tiếp cận bị loại

| Phương án | Vì sao không chọn |
|---|---|
| Đổi mã `PAYLOAD_TOO_LARGE` thành `USER_AVATAR_TOO_LARGE` ở **mọi** route | Biến một request JSON quá lớn thành lỗi ảnh. Tệ hơn lỗi gốc: một route không liên quan gì tới ảnh lại trả lý do về ảnh, và client sẽ đi nén một JSON |
| Để `PAYLOAD_TOO_LARGE` như cũ, chấp nhận hai mã cho cùng một lỗi | Chính là lỗi đang sửa. Không thêm tham số nào cũng không sửa được việc client phải đoán mình sẽ nhận mã nào |
| Chỉ ghi log cảnh báo khi trần toàn cục thấp hơn trần avatar | Vẫn là cấu hình sai đang chạy trên production: mọi upload hỏng và operator phải tự tìm ra nguyên nhân từ một `413` không gợi ý gì. ADR 008 đã chấp nhận rủi ro này ở dạng ghi chú; feature này chọn chặn |
| Nâng `MAX_BODY_BYTES` lên một giá trị rất lớn để "chắc chắn đủ" | Biến chặn sớm thành vô nghĩa: một endpoint khác sẽ đệm tới giá trị đó trước khi bị từ chối. Đây đúng là kiểu lựa chọn mà trần thô sinh ra để chặn |
| Đặt trần avatar vào một biến môi trường riêng cho dễ chỉnh | Constitution I cấm tầng dùng chung import module; biến môi trường cũng không giải quyết được việc tầng pipeline phải biết trần của một module. Nó chỉ đổi một hằng số thành một cấu hình có thể sai |
| Đặt guard trong `ValidateForAPI` của `config` | `config` không được biết route avatar tồn tại, và `ValidateForAPI` chạy **sau** `Load()`, khi biến môi trường đã parse xong — chỗ đúng để so sánh hai con số là composition root, nơi duy nhất thấy cả hai |
| Đặt guard trong `cmd/api` và test ở đó | `run()` mở database và chạy migration trước khi tới chỗ cần kiểm, nên một test ở đó phải cần hạ tầng thật để khẳng định một phép so sánh thuần kỹ thuật. Guard vì thế nằm ở package của route và được test không cần database |

## 5. Bổ sung (2026-10-06)

Khi chạy `quickstart.md` end to end cho feature này, kịch bản upload avatar thật **không
chạy được** trên cấu hình mặc định của máy dev: `MEDIA_CLOUD_NAME` bị đặt bằng giá trị
của `MEDIA_FOLDER`, và provider trả `401`. Hành vi của service là đúng — `503
USER_MEDIA_UNAVAILABLE`, hồ sơ không đổi, log chỉ ghi `the provider refused the request
(status 401)` chứ không ghi câu của provider — nhưng đây là một mục **cấu hình**, và nó
đã được ghi thành quy trình lấy giá trị trong `docs/configuration.md`. Chi tiết về việc
`MEDIA_CLOUD_NAME` không phải tên folder: mục *Xác minh với nhà cung cấp thật*.

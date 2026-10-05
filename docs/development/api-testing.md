# API Testing

Cách kiểm thử endpoint bằng tay, từ khởi động stack đến nghiệm thu luồng
end-to-end. Dùng cho cả việc tự kiểm trước khi gửi và tài liệu nghiệm thu.

Danh sách endpoint và hợp đồng: [../api-reference.md](../api-reference.md).
Kịch bản cụ thể cho từng feature nằm trong `specs/NNN-*/quickstart.md`.

## 1. Khởi động

```bash
make up            # api + db + redis, không mở port db/redis
make up-tools      # thêm port host + Mailpit (http://localhost:8025)
```

Kiểm tra sẵn sàng trước khi test:

```bash
curl -s http://localhost:8080/healthz   # {"status":"alive"}
curl -s http://localhost:8080/readyz    # {"status":"ready", ...}
```

`readyz` trả 503 khi DB, schema hoặc Redis chưa dùng được ⇒ không test nghiệp vụ lúc
đó, hãy xem [../troubleshooting.md](../troubleshooting.md).

## 2. Lấy token

```bash
BASE=http://localhost:8080/api/v1

ACCESS=$(curl -s -X POST $BASE/auth/login \
  -H 'Content-Type: application/json' \
  -d '{"email":"user@example.com","password":"Str0ng!Pass"}' \
  | sed -E 's/.*"accessToken":"([^"]+)".*/\1/')

AUTH="Authorization: Bearer $ACCESS"
```

Trong PowerShell:

```powershell
$resp = Invoke-RestMethod -Method Post -Uri "$BASE/auth/login" `
  -ContentType 'application/json' `
  -Body '{"email":"user@example.com","password":"Str0ng!Pass"}'
$AUTH = @{ Authorization = "Bearer $($resp.data.accessToken)" }
```

## 3. Bốn kiểm tra phải làm với **mọi** endpoint

> Hiện mới có module 01, nên các ví dụ dùng `$BASE/auth/*`. Khi module 02 (`user`)
> có rồi thì thay bằng `$BASE/users/me` cho đúng contract của module đó.

### a) Không token ⇒ `401`

```bash
curl -s -o /dev/null -w "%{http_code}\n" $BASE/auth/me     # 401
```

### b) Sai phân quyền ⇒ `403`, và phải có audit

```bash
# token CUSTOMER gọi route ADMIN
curl -s $BASE/auth/admin/probe -H "$AUTH"
```

Sau đó kiểm tra đã ghi audit:

```bash
docker compose exec -T db psql -U app -d artist_shop \
  -c "SELECT action, outcome FROM audit_logs ORDER BY created_at DESC LIMIT 3;"
```

### c) Truy cập chéo ⇒ không lộ dữ liệu

Đây là kiểm tra quan trọng nhất. Tạo 2 tài khoản, rồi thử đụng tài nguyên của
tài khoản kia:

```bash
curl -s -X PATCH $BASE/users/me/addresses/<ID_CỦA_TÀI_KHOẢN_KHÁC> \
  -H "$AUTH2" -H 'Content-Type: application/json' -d '{"streetAddress":"hack"}'
# Kỳ vọng: 404, không phải 403 và không phải 200
```

Kỳ vọng đúng là **404** chứ không phải 403: nếu trả 403, client có thể dò ra địa chỉ đó
tồn tại.

### d) Envelope đúng chuẩn

Mọi response phải có `data`/`meta` hoặc `error`/`requestId`:

```bash
curl -s $BASE/auth/me -H "$AUTH" | python -m json.tool
```

## 4. Kiểm tra validation

```bash
# Sai kiểu / thiếu trường / field lạ
curl -s -X PATCH $BASE/users/me -H "$AUTH" -H 'Content-Type: application/json' \
  -d '{"phone":"abc"}'
```

Kỳ vọng: `400` với `error.code` ổn định và `error.details[]` chỉ đúng field sai:

```json
{
  "error": {
    "code": "USER_INVALID_PHONE",
    "message": "...",
    "details": [{ "field": "phone", "issue": "..." }],
    "requestId": "..."
  }
}
```

Client phải bám `error.code`, **không** bám `error.message`.

## 5. Kiểm tra trạng thái sau khi thao tác

Sau mỗi thay đổi dữ liệu, hãy đọc lại và so với kỳ vọng — đừng chỉ tin mã `200`.

```bash
curl -s $BASE/auth/me -H "$AUTH"                       # account đúng chưa
docker compose exec -T db psql -U app -d artist_shop \
  -c "SELECT user_id, count(*) FROM addresses
       WHERE is_default AND deleted_at IS NULL
       GROUP BY user_id HAVING count(*) > 1;"          # ràng buộc: 0 dòng
```

Câu SQL thứ hai kiểm tra ràng buộc ở **tầng lưu trữ**, không phải ở code.

## 6. Kiểm tra bất biến dưới tải đồng thời

Với ràng buộc "đúng một địa chỉ mặc định", hãy gửi hai request song song:

```bash
for id in $A $B; do
  curl -s -X POST $BASE/users/me/addresses/$id/default -H "$AUTH" > /dev/null &
done; wait

# Kỳ vọng: truy vấn ở bước 5 trả về 0 dòng
```

## 7. Correlation ID

Mỗi response trả header `X-Request-Id`, và giá trị đó xuất hiện trong
`meta.requestId` / `error.requestId`:

```bash
curl -si $BASE/auth/me -H "$AUTH" | grep -i x-request-id
curl -si $BASE/auth/me -H 'X-Request-Id: 5c9f1a1e-6f0e-4a0e-9c3b-2f6a1b8c7d90' -H "$AUTH" \
  | grep -i x-request-id   # giữ nguyên giá trị hợp lệ
```

Dùng nó khi báo lỗi để tra log.

## 8. Upload

```bash
curl -s -X POST $BASE/users/me/avatar -H "$AUTH" -F "file=@avatar.jpg;type=image/jpeg"

# sai loại: đổi tên file nhưng nội dung không phải ảnh
cp avatar.jpg fake.jpg && echo "not an image" > fake.png
curl -s -X POST $BASE/users/me/avatar -H "$AUTH" -F "file=@fake.png"
```

Luôn kiểm tra **nội dung** bị từ chối đúng và trạng thái cũ **không** bị đổi.

## 9. Script nghiệm thu nhanh

Khi một feature có `quickstart.md`, chạy đúng các lệnh trong đó và ghi lại kết quả
quan sát được vào báo cáo. Đây là bước bắt buộc ở phase Polish
(`tasks.md` có task tương ứng cho mỗi feature).

## 10. Khi test thủ công phát hiện lỗi

1. Ghi lại request + response đầy đủ (che token, mật khẩu).
2. Kiểm tra đây là lỗi hành vi hay lỗi tài liệu.
3. Nếu là lỗi hành vi: sửa code **và** cập nhật `docs/api-reference.md` trong cùng
   thay đổi (Constitution VIII).
4. Thêm test tự động tái hiện lỗi đó — lỗi tìm bằng tay mà không có test sẽ quay
   lại.
# Quickstart: Address Facebook URL (validation guide)

**Feature**: `021-address-facebook-url` — chỉ kiểm thử, không chứa code triển khai.

## Chuẩn bị

- DB + Redis chạy (`make up` hoặc stack tương đương), đã `make migrate`.
- Có 2 token: khách hàng (`CUSTOMER`) và admin (`ADMIN`).
- Base URL: `http://localhost:8080/api/v1` (gọi là `$BASE`).

## Kịch bản kiểm thử

### 1. Tạo địa chỉ thiếu link → bị từ chối, không lưu gì

```bash
curl -s -X POST $BASE/users/me/addresses \
  -H "Authorization: Bearer $CUSTOMER" -H "Content-Type: application/json" \
  -d '{"recipientName":"Nguyen Van A","recipientPhone":"0912345678",
       "provinceCode":"01","wardCode":"00001","streetAddress":"12 Hai Ba Trung"}'
# Kỳ vọng: 400, error.code = VALIDATION_ERROR, details[].field = facebookUrl
# Gọi GET /users/me/addresses: tổng số không tăng.
```

### 2. Tạo địa chỉ sai link → bị từ chối đúng field

Lặp với từng `facebookUrl`: `http://facebook.com/a` (sai scheme), `ftp://facebook.com/a`, `https://example.com/a`, `https://myfacebook.com/a`, `https://faceboook.com/a`, `https://facebook.com.evil.com/a`, `facebook.com/a` (thiếu scheme), `https://facebook.com` và `https://facebook.com/` (host trần không path), `https://facebook.com/unknown` (giữ chỗ), chuỗi 501 ký tự.
Kỳ vọng chung: `400 VALIDATION_ERROR`, `details[].field = facebookUrl`, không lưu địa chỉ.
Case chấp nhận (đối chứng): `https://www.facebook.com/a`, `https://m.facebook.com/a`, `https://fb.com/a`, host viết hoa, port tường minh → `201`.

### 3. Tạo địa chỉ đúng → 201 và đọc lại có link

```bash
curl -s -X POST $BASE/users/me/addresses \
  -H "Authorization: Bearer $CUSTOMER" -H "Content-Type: application/json" \
  -d '{"recipientName":"Nguyen Van A","recipientPhone":"0912345678",
       "provinceCode":"01","wardCode":"00001","streetAddress":"12 Hai Ba Trung",
       "facebookUrl":"https://facebook.com/nguyen.van.a"}'
# Kỳ vọng: 201, data.facebookUrl đúng giá trị gửi.
# GET /users/me/addresses và GET /users/{id} (admin) đều hiện facebookUrl.
```

### 4. Sửa địa chỉ: vắng mặt giữ cũ, rỗng bị từ chối, đổi hợp lệ được chấp nhận

- `PATCH .../addresses/{id}` không gửi `facebookUrl` trên địa chỉ thường + đổi `streetAddress` → `200`, link cũ giữ nguyên.
- `PATCH` bất kỳ trường nào trên địa chỉ đang mang giữ chỗ `https://facebook.com/unknown` (kể cả vắng mặt `facebookUrl`) → `400 VALIDATION_ERROR` field `facebookUrl`.
- `PATCH` với `"facebookUrl":""` → `400 VALIDATION_ERROR` field `facebookUrl`, đọc lại địa chỉ cũ nguyên vẹn (kể cả `isDefault`).
- `PATCH` với `"facebookUrl":"https://fb.com/nguyen.van.b"` → `200`, link mới.

### 5. Checkout chụp link; sửa địa chỉ không đổi đơn cũ; địa chỉ giữ chỗ bị chặn

- `POST $BASE/orders {"addressId":"<id-co-link-that>"}` → `201`, chi tiết đơn có `address.facebookUrl` đã chụp.
- `PATCH` địa chỉ đổi link → đọc lại đơn cũ: `address.facebookUrl` giữ nguyên.
- Địa chỉ mang `https://facebook.com/unknown` (dữ liệu cũ): checkout/sửa đơn đổi sang nó → `400 VALIDATION_ERROR` field `facebookUrl`.

## Lệnh gate

```bash
make lint
make test
make test-integration
# close-out:
make check   # bao gồm make swagger-check + build
```

Chi tiết schema xem [contracts/openapi-addition.yaml](contracts/openapi-addition.yaml),
mã lỗi xem [contracts/error-codes.md](contracts/error-codes.md),
mô hình dữ liệu xem [data-model.md](data-model.md).

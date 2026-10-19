# Quickstart: Order SSE (020)

```bash
# 1. Login lấy token
curl -s -X POST localhost:8080/api/v1/auth/login \
  -H 'Content-Type: application/json' \
  -d '{"email":"user@example.com","password":"Str0ng!Pass"}'

# 2. Mở stream khách (giữ terminal mở) — dùng fetch vì EventSource không gắn header:
# Node 18+ (thay TOKEN bằng access token ở bước 1):
node -e 'const t="TOKEN";fetch("http://localhost:8080/api/v1/orders/stream",{headers:{Authorization:`Bearer ${t}`}}).then(async r=>{for await (const c of r.body) process.stdout.write(c)})'
```

Admin: đổi `/orders/stream` thành `/admin/orders/stream` với token admin.

Kiểm tra nhanh: mở stream ở terminal 1, checkout/confirm ở terminal 2, xem event về trong < 5s.

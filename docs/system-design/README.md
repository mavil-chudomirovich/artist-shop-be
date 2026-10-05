# System Design

Tài liệu thiết kế: **kiến trúc có thể tra cứu và phải tuân thủ**. Quy trình phát
triển nằm ở [`../development/`](../development/README.md).

| Tài liệu | Nội dung |
|---|---|
| [architecture.md](../architecture.md) | Kiến trúc tổng thể (authoritative) |
| [design-pattern.md](design-pattern.md) | Các pattern dự án dùng, kèm ranh giới áp dụng |
| [contract-purity.md](contract-purity.md) | Luật cho hợp đồng liên module |

## Vì sao có `design-pattern.md`

Một pattern không phải luôn luôn tốt. Mỗi mục trong
[design-pattern.md](design-pattern.md) ghi rõ **pattern dùng ở đâu** và **khi nào
không nên dùng**, để người sau không lạm dụng nó.

## Thứ tự ưu tiên khi mâu thuẫn

```
constitution  >  docs/system-design  >  docs/development  >  tài liệu khác  >  code
```

Nếu code mâu thuẫn với tài liệu, tài liệu thắng và code phải được sửa cho khớp.
Quyết định đã chốt nằm trong [`../decisions/`](../decisions/README.md).
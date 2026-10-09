# ADR 014 - Tồn kho: hai hợp đồng liên module, mô hình ba bảng, và sweeper nhả giữ chỗ

- **Status**: Accepted
- **Decision Date**: 2026-10-09
- **Decision Maker**: Mavil
- **Feature**: `specs/007-inventory-tracking`

## 1. Bối cảnh

Module 05 Inventory giao tồn kho và lịch sử biến động kho, và ba quyết định trong feature này sẽ
khiến người đọc code sau phải dừng lại, vì mỗi quyết định đều có một lựa chọn thay thế trông hợp lý
hơn ở bề mặt:

1. **Vì sao inventory nói chuyện với product qua hai interface trong `internal/contracts`, và vì sao
   adapter lại được viết ở phía product?** Đây là module **đầu tiên** vừa phụ thuộc product, vừa
   **buộc product đổi dữ liệu của chính nó**: khả dụng chạm 0 thì product phải ngừng bán. FR-024–FR-028
   đòi hệ quả đó xảy ra **trong cùng transaction** với thay đổi kho, nên hai module phải nói chuyện mà
   không module nào đọc bảng của module kia (Hiến pháp I).
2. **Vì sao ba bảng (`stock_levels`, `inventory_transactions`, `stock_holds`) trong khi module doc chỉ
   nêu một (`inventory_transactions`)?** Luật "không âm" phải đứng vững khi hai thao tác chạm cùng sản
   phẩm cùng lúc (FR-010), và một giữ chỗ không phải một biến động vật lý (FR-004).
3. **Vì sao có một tiến trình nền (sweeper) — thứ chưa từng có trong dự án?** Một giữ chỗ phải **tự
   hết hạn** sau 15 phút kể cả khi không ai chạm lại sản phẩm đó (FR-015).

Ba hệ quả này quyết định cách hai module được phép nói chuyện, dữ liệu tồn kho được lưu thế nào, và
điều gì chạy nền, nên chúng được ghi thành ADR thay vì chỉ nằm trong `research.md` của feature.

## 2. Quyết định

### 2.1 Hai hợp đồng liên module, adapter viết ở phía product

Module 05 khai **hai** interface trong `internal/contracts/product.go`, và **module 04 cung cấp một
adapter** cho cả hai ở composition root:

```go
// ProductAvailability: lời ra lệnh (signal), không phải câu hỏi. Product ánh xạ nó lên
// hai cạnh state machine của chính nó, chạy trong transaction của caller.
type ProductAvailability interface {
    MarkOutOfStock(ctx context.Context, productID uuid.UUID) error
    MarkOnSale(ctx context.Context, productID uuid.UUID) error
}

// ProductLookup: câu hỏi "sản phẩm có tồn tại không", thứ khóa ngoại không trả lời được
// vì nó chỉ nổ khi INSERT, còn đường đọc và đường giảm thì không ghi dòng nào.
type ProductLookup interface {
    ProductExists(ctx context.Context, productID uuid.UUID) (bool, error)
}
```

Adapter nằm ở `product/infrastructure/implement/availability/` vì **product là bên sở hữu sell state
và bên cung cấp** theo Hiến pháp I: hợp đồng được khai ở `internal/contracts` (gói này **không**
import module nào), và module **cung cấp** hợp đồng viết adapter. Hướng phụ thuộc vẫn một chiều —
`inventory → product` — đúng như roadmap. Signal đi qua một use case **hệ thống riêng**
(`product/application/implement/availability.go`), không dùng lại `ChangeSellState` của admin: đường
admin cần và audit một admin, còn signal không có tác nhân người và audit với `nil` actor.

Hợp đồng nói bằng **ngôn ngữ khả dụng** ("out of stock", "on sale"), không bằng tên bốn trạng thái
của product, nên inventory không bao giờ học được state machine của product; product tự ánh xạ hai
signal lên đúng hai cạnh `SellOut`/`Restock` mà feature 006 đã để sẵn.

### 2.2 Mô hình ba bảng, với dòng level "sống" làm chốt chặn đồng thời

Ba bảng lưu ba sự thật khác nhau về cùng một sản phẩm:

| Bảng | Lưu gì | Vai trò |
|---|---|---|
| `stock_levels` | một số vật lý cho mỗi sản phẩm | **Chốt chặn đồng thời**: mọi lần giảm là `UPDATE ... WHERE quantity >= $n RETURNING quantity`, nên luật không âm được Postgres tự đánh giá lại dưới khóa ghi của nó |
| `inventory_transactions` | ledger append-only của mọi biến động **vật lý** | Lịch sử truy vết; mỗi dòng mang `delta` có dấu và `resulting_quantity` |
| `stock_holds` | phần đang giữ cho một đơn đang thanh toán | Khác biệt giữa "trên kệ" và "khả dụng"; hết hạn sau 15 phút |

**Khả dụng không được lưu.** Nó là `stock_levels.quantity − Σ stock_holds` đang hoạt động, được tính
tại thời điểm hỏi (FR-013), nên không thể lệch khỏi các dòng nó sinh ra. Mỗi thay đổi vật lý, dòng
ledger của nó, và hệ quả khả dụng của nó nằm trong **một** transaction (`UnitOfWork`, FR-011).

Bất biến đối chiếu SC-001: vì mỗi kind lưu `delta` có dấu và level được cập nhật đúng bằng delta đó
trong cùng transaction, `SUM(delta)` trên các movement của một sản phẩm luôn bằng
`stock_levels.quantity`. Giữ chỗ không đổi cả hai, nên không phá được đẳng thức; chỉ khoảnh khắc một
giữ chỗ chuyển thành `SALE` mới ghi một movement và hạ level cùng lúc.

Hai luật không âm là **ràng buộc ở tầng lưu trữ** (FR-010): `CHECK (quantity >= 0)` và
`CHECK (resulting_quantity >= 0)` là thuộc tính một cột nên là `CHECK`; còn "giảm vật lý không được
thấp hơn phần đang giữ" là quan hệ giữa hai bảng, không `CHECK` nào diễn đạt được, nên được làm
**race-free** bằng chính khóa dòng `SELECT ... FOR UPDATE` mà đường reserve đã lấy.

### 2.3 Sweeper nhả giữ chỗ hết hạn

Composition root (`cmd/api/main.go`) khởi động một `time.Ticker` loop
(`inventory/presentation/worker/sweeper.go`) gọi use case `ExpireHolds` mỗi **30 giây** và dừng khi
context bị huỷ. Sweeper **không mang luật nghiệp vụ nào**: nó chỉ biết gọi use case theo lịch; luật
"hết hạn sau 15 phút" nằm trong domain/application.

Đọc khả dụng và tiêu thụ giữ chỗ cũng coi một giữ chỗ chỉ còn hoạt động khi `status = 'ACTIVE' AND
expires_at > $now`, với `$now` là **instant của `Clock` được inject**, truyền vào SQL **như tham số**
— không bao giờ dùng `now()` của database. Nhờ vậy sweeper và phép tính khả dụng **đồng ý** với nhau
về "đã hết hạn nghĩa là gì", và một giữ chỗ hết hạn mà sweeper chưa quét vẫn không chặn khả dụng,
cũng không thể bị tiêu thụ.

## 3. Hệ quả

| Hệ quả | Chi tiết |
|---|---|
| Tích cực | Ranh giới module được giữ: inventory không đọc `products`, product không đọc bảng inventory; cả hai nói qua hợp đồng khai một lần ở `internal/contracts` |
| Tích cực | Luật không âm đứng vững dưới đồng thời vì nó là **câu lệnh UPDATE có điều kiện**, không phải kiểm tra đọc-rồi-ghi ở Go; một oversell khớp 0 dòng nên thay đổi không gì và không ghi ledger |
| Tích cực | Khả dụng là dữ kiện dẫn xuất nên không có bản sao nào lệch; "đã bán" và "đang giữ" phân biệt được, điều mà một con số duy nhất không làm được (đó là lý do FR-016 nói trả tiền không đổi khả dụng) |
| Tích cực | Sweeper bảo đảm **giải phóng cuối cùng** kể cả với sản phẩm không ai chạm lại — đúng ca nguy hiểm nhất: đơn vị cuối bị giữ mà không bao giờ được nhả thì sản phẩm kẹt `OUT_OF_STOCK` vĩnh viễn |
| Tiêu cực | **Inventory tạo ra một phụ thuộc thật vào product.** Contract và adapter phải tồn tại và được wire ở composition root; một product chưa wire nghĩa là inventory không hoàn tất được thao tác (signal chạy trong cùng transaction, nên lỗi signal rollback cả thay đổi kho) |
| Tiêu cực | **Một transaction inventory có thể ghi sang module product.** Đây là điều Hiến pháp II và FR-026 buộc: nếu signal chạy ngoài transaction, một crash giữa hai bước để lại sản phẩm còn bán trong khi kho đã hết |
| Tiêu cực | **Ba bảng thay vì một.** Dựng level từ ledger là bất khả thi vì không có dòng để khóa; ghi giữ chỗ vào ledger sẽ vi phạm FR-004 và phá đẳng thức SC-001 |
| Tiêu cực | **Sweeper là cơ chế mới, thêm một goroutine và một vòng đời start/stop.** Nhưng nó là cách nhỏ nhất để một giữ chỗ tự nhả; lazy-release thuần sẽ khiến sản phẩm không traffic kẹt trạng thái sai |
| Tiêu cực | **Quan hệ "không thấp hơn phần đang giữ" không phải `CHECK`.** Nó dựa vào khóa dòng, nên đúng vì mọi đường ghi đều đi qua khóa đó — một đường ghi tương lai quên khóa sẽ phá vỡ nó |

**Xem lại quyết định khi**: module 07 Order cần một hợp đồng đặt chỗ liên module (khi đó hợp đồng
`InventoryReservation` được thêm và shape của nó phải theo nhu cầu Order, không đoán trước —
`specs/007-inventory-tracking/deferred.md` D1); hoặc khi cần kho nhiều địa điểm (khi đó
`stock_levels` phải mang thêm chiều vị trí).

## 4. Cách tiếp cận bị loại

| Phương án | Vì sao không chọn |
|---|---|
| Một hợp đồng `Transition(productID, toState)` chung | Làm lộ tên bốn trạng thái của product sang inventory và cho inventory gọi một target bất hợp lệ; signal theo ngôn ngữ khả dụng giữ state machine ở đúng nơi sở hữu nó |
| Inventory đọc/ghi thẳng bảng `products` | Hiến pháp I cấm một module chạm bảng của module khác |
| Product đăng ký nghe sự kiện inventory | Không có hệ thống event nào trong dự án, và nó đảo hướng phụ thuộc roadmap |
| Dùng lại `ChangeSellState` của admin cho signal | Đường admin đòi và audit một tác nhân admin; dùng lại sẽ phải giả một admin cho một thay đổi không do người gây ra |
| Product phải hỏi inventory lúc bật bán | Sinh phụ thuộc ngược/vòng (clarification đã chốt); chỉ khoảnh khắc cắt qua 0 mới tự chuyển |
| Dựng `stock_levels.quantity` bằng tổng ledger | Không có dòng để khóa, nên hai writer cùng đọc một tổng và cùng ghi → kho âm — đúng race mà FR-010 cấm |
| Ghi giữ chỗ như movement âm trong ledger | FR-004 nói giữ chỗ không phải biến động vật lý; trộn vào sẽ phá đẳng thức SC-001 và làm "đã bán" lẫn "đang giữ" |
| Lazy-release thuần (nhả khi khả dụng được tính lần tới) | Sản phẩm không nhận traffic nữa sẽ không bao giờ trở lại bán, nên sell state sai vô thời hạn; và một GET sẽ phải ghi (đổi contract của đường đọc) |
| Dùng `pg_cron` / job trong database | Đặt luật nghiệp vụ ra ngoài ứng dụng và ngoài tầm test |
| Dùng `RESTRICT` để lịch sử không mất khi xóa sản phẩm | Sẽ khiến hard delete hiện có của product bắt đầu thất bại với mọi sản phẩm từng có kho — thay đổi hành vi module 04 không ai yêu cầu; xem xét lại cùng module 07 Order (`deferred.md` D3) |

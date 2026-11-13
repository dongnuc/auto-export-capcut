# CapCut Auto Exporter

Xuất hàng loạt project CapCut bằng **chính CapCut**. Tool chỉ điều khiển ứng dụng: mở project → phím xuất → xác nhận → chờ file xuất xong → `Ctrl+W` → project tiếp theo. Tool không dùng chung code với phần còn lại của repo.

## Build
```
go build -ldflags "-H=windowsgui" -o capcut-auto-exporter.exe .
go test ./...
go run . -list        # in danh sách project
```

## Thư mục output: tự lấy từ CapCut
CapCut lưu thư mục xuất lần gần nhất **một chỗ cho cả ứng dụng** trong registry (không nằm trong thư mục project):
`HKEY_CURRENT_USER\Software\Bytedance\CapCut\Modules\Export` → `ExportPath` (kèm `Resolution`, `codec`). Đã kiểm chứng: hộp thoại xuất bắt đầu từ giá trị này, và nó cập nhật theo thư mục bạn chọn khi xuất.

Vì vậy ô **Theo CapCut** (bật mặc định) cạnh *Thư mục output* tự điền thư mục này và đọc lại trước mỗi project; không cần cấu hình output nữa. Muốn đổi nơi xuất: chọn thư mục khác trong hộp thoại xuất của CapCut một lần. Bỏ tích ô này nếu muốn nhập thư mục tay (tool sẽ cảnh báo nếu khác thư mục CapCut sẽ xuất).

## Chuẩn bị (làm một lần)
1. Trong CapCut, xuất thử một video vào **thư mục output** bạn sẽ dùng. CapCut sẽ nhớ thư mục này, và nó phải trùng với "Thư mục output" trong tool. Tên file để mặc định (= tên project).
2. Kiểm tra phím tắt xuất trong CapCut (mặc định trong tool: `Ctrl+E`), đóng project `Ctrl+W`.
3. Chế độ **Tự động**: bấm **Mở CapCut** → **Hiệu chỉnh vị trí thẻ** → trong 5 giây, đưa chuột vào giữa thẻ project đầu tiên ở màn hình chính CapCut.

## Cách tool chạy (mỗi project)
| Bước | Việc làm |
|---|---|
| Kiểm tra | Đã có `<tên>.mp4` trong output → bỏ qua (hoặc đổi tên file cũ thành `.bak`) |
| Mở (tự động) | Đóng CapCut, sửa `tm_draft_modified` để project lên đầu danh sách (sao lưu `root_meta_info.json` trước), mở CapCut, click thẻ đầu tiên |
| Mở (thủ công) | Bạn tự mở project rồi bấm "Đã mở — tiếp tục" |
| Xuất | `Escape` → phím xuất → chờ hộp thoại → `Enter` |
| Chờ | File mới khớp tên + không còn bị khóa + dung lượng đứng yên + thời lượng ≈ project (dùng `ffmpeg.exe` đi kèm CapCut) |
| Đóng | `Escape` → `Ctrl+W` → (tự động) thoát CapCut |
| Lỗi | Không thấy file sau N giây (popup Pro, hộp thoại lạ…) hoặc quá thời gian → ghi log, đóng, sang project tiếp |

Dừng khẩn cấp: **Ctrl+Alt+Q** (dùng được cả khi CapCut đang ở phía trước). Trong lúc chạy, không dùng chuột hoặc bàn phím và không khóa màn hình.

Cài đặt lưu ở `%APPDATA%\capcut-auto-exporter\config.json`. Các thông số nâng cao (timeout, hệ số thời gian, độ lệch thời lượng…) chỉnh trong file này.

## Log và chẩn đoán lỗi
- Mỗi lần chạy tạo `%APPDATA%\capcut-auto-exporter\logs\run-<ngày-giờ>.log` (đủ mức DEBUG: từng phím, từng click, tọa độ, cửa sổ CapCut, popup) và thư mục `run-<ngày-giờ>-screens\` chứa ảnh chụp từng bước và ảnh lúc lỗi. Nút **Thư mục log** trên giao diện mở thư mục này.
- Lỗi ghi rõ bước: `[mo-capcut]`, `[click-the]`, `[mo-project]`, `[sau-khi-mo]` (popup, thiếu media), `[phim-xuat]`, `[xac-nhan]`, `[cho-file]`.
- Project thiếu file phương tiện (popup "Liên kết tệp phương tiện") bị bỏ qua mặc định; đặt `"missing_media": "continue"` để vẫn xuất.
- Chạy không giao diện: `capcut-auto-exporter.exe -export "0609,Boxing"` (dùng cài đặt đã lưu).

## Hành vi CapCut 6.9 đã đo
- Màn hình chính và editor là 2 cửa sổ khác nhau; tool biết project đã mở khi cửa sổ chính đổi.
- Hộp thoại xuất là cửa sổ riêng tên "Xuất"; `Enter` bấm được nút Xuất. Dự phòng: click nút tại `export_btn_x/y`.
- Sau khi xuất xong, CapCut **giữ file mở** cho tới khi thoát, nên tool xác nhận xong bằng dung lượng đứng yên + thời lượng (ffmpeg).
- CapCut có thể mở trên màn hình phụ; tool đưa về màn hình chính và phóng to trước khi click (`move_to_primary`).

## Đổi tên project
- Bấm **…** ở cuối dòng → **Đổi tên** → sửa tên → **Lưu** (hoặc Enter). CapCut phải đang **đóng**, nếu không CapCut sẽ ghi đè lại danh sách.
- Tool làm giống hệt chức năng "Đổi tên" của CapCut (đã đối chiếu bằng cách tự đổi tên trong CapCut rồi so sánh file): **đổi tên thư mục project** và cập nhật `draft_name`, `draft_fold_path`, `draft_json_file` trong `root_meta_info.json` (có sao lưu trước). Chỉ đổi `draft_name` là không đủ: khi mở project, CapCut lấy tên từ thư mục và ghi đè về tên cũ.
- Tên mới là tên hiển thị trong CapCut **và** tên file khi xuất.
- Không chấp nhận tên trống, trùng project/thư mục khác, tên dành riêng của Windows (CON, NUL…), hoặc chứa `< > : " / \ | ? *`. Lỗi giữa chừng sẽ khôi phục lại tên thư mục cũ.
- Dòng lệnh: `capcut-auto-exporter.exe -rename "tên cũ=>tên mới"`.

## Tải lại danh sách project
Nút **Tải lại** (cạnh ô Thư mục project, và ở đầu danh sách) đọc lại `root_meta_info.json` trong thư mục đã chọn, giữ nguyên các project đang tích chọn và trạng thái đã chạy. Tool cũng tự tải lại sau mỗi lần xuất, vì CapCut ghi lại tên và thời gian khi mở project.

## Tắt máy khi xuất xong
- Tích **Tắt máy khi xuất xong** trước khi bấm Bắt đầu (không được lưu lại cho lần mở sau, để tránh tắt máy ngoài ý muốn).
- Khi hàng đợi chạy hết, tool gọi `shutdown /s /t 60`: máy tắt sau 60 giây, bấm **Hủy tắt máy** (hoặc chạy `shutdown /a`) để hủy.
- Không tắt máy nếu hàng đợi bị dừng giữa chừng (nút Dừng, Ctrl+Alt+Q, hoặc CapCut còn đang xuất khi có lỗi).

## "Đổi tên file cũ thay vì bỏ qua"
Trước khi xuất mỗi project, tool xem thư mục output đã có video trùng tên project chưa (ví dụ `Boxing.mp4`, `Boxing (1).mp4`).
- **Tắt** (mặc định): project đó được đánh dấu *Bỏ qua* — không xuất lại, file cũ giữ nguyên.
- **Bật**: file cũ được đổi tên thành `Boxing.mp4.<ngày-giờ>.bak` (không xóa), rồi xuất lại bản mới thành `Boxing.mp4`.

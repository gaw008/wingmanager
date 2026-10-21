package web

import "github.com/gaw008/wingmanager/internal/settings"

func init() {
	values := map[string][2]string{
		"booking_check_out_today": {"今天退房", "Checks out today"},
		"booking_check_in_today":  {"今天入住", "Checks in today"},
		"booking_checked_out":     {"已退房", "Checked out"},
		"booking_ended":           {"已结束", "Ended"},
		"booking_details":         {"订单详情", "Booking details"},
		"booking_edit_guest":      {"编辑客人与备注", "Edit guest and notes"},
		"booking_manual_notice":   {"手动创建的订单，所有信息都可以修改。", "This booking was created manually. All information can be edited."},
		"booking_import_notice":   {"从平台日历导入。日期和状态由平台决定，这里只能修改客人信息和备注。", "Imported from a platform calendar. The platform owns dates and status; only guest information and notes can be edited here."},
		"booking_overlap_notice":  {"和同一单元的其他订单或租约日期重叠。请核对来源；导入订单需在平台处理，下次同步会更新这里。", "These dates overlap another booking or lease for this unit. Review the source; imported bookings must be resolved on the platform and will update on the next sync."},
		"booking_cleaning_notice": {"保存入住订单后，会在退房日自动生成保洁任务。", "Saving a stay automatically creates a cleaning task due on checkout."},
		"booking_no_cleaning":     {"无保洁任务", "No cleaning task"},
		"booking_no_notes":        {"暂无备注", "No notes"},
		"booking_sync":            {"日历同步", "Calendar sync"},
		"booking_list":            {"订单", "Bookings"},
		"booking_sync_ok":         {"同步正常", "Sync successful"},
	}
	for key, pair := range values {
		catalogs[settings.LocaleChinese][key] = pair[0]
		catalogs[settings.LocaleEnglish][key] = pair[1]
	}
}

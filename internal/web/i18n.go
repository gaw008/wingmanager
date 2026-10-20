package web

import "github.com/gaw008/wingmanager/internal/settings"

var catalogs = map[string]map[string]string{
	settings.LocaleChinese: {
		"today": "今天", "settings": "设置", "login": "登录", "logout": "退出登录",
		"setup": "创建房东账号", "email": "邮箱", "password": "密码", "create_account": "创建账号",
		"setup_intro": "首次使用，请创建唯一的房东账号。", "landlord_name": "房东姓名",
		"timezone": "时区", "currency": "币种", "locale": "语言", "save": "保存",
		"invalid_account": "请输入有效邮箱和非空密码（最多 72 字节）。", "invalid_credentials": "邮箱或密码错误。",
		"invalid_timezone": "请输入有效的 IANA 时区，例如 America/Los_Angeles。",
		"invalid_locale":   "请选择支持的语言。", "invalid_currency": "请填写币种。",
		"internal_error": "操作失败，请查看服务器日志。", "invalid_form": "无法读取表单。",
	},
	settings.LocaleEnglish: {
		"today": "Today", "settings": "Settings", "login": "Log in", "logout": "Log out",
		"setup": "Create owner account", "email": "Email", "password": "Password", "create_account": "Create account",
		"setup_intro": "Welcome. Create the single owner account to get started.", "landlord_name": "Landlord name",
		"timezone": "Timezone", "currency": "Currency", "locale": "Language", "save": "Save",
		"invalid_account": "Enter a valid email and a non-empty password (at most 72 bytes).", "invalid_credentials": "Incorrect email or password.",
		"invalid_timezone": "Enter a valid IANA timezone, such as America/Los_Angeles.",
		"invalid_locale":   "Choose a supported language.", "invalid_currency": "Enter a currency.",
		"internal_error": "Operation failed. Check the server log.", "invalid_form": "Could not read the form.",
	},
}

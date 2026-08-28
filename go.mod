module github.com/dusto/tend-ui

go 1.26.3

replace github.com/webview/webview_go => ./third_party/webview_go

replace github.com/dusto/tend => ../tend

require (
	github.com/a-h/templ v0.3.1020
	github.com/webview/webview_go v0.0.0-00010101000000-000000000000
)

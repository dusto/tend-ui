module github.com/dusto/tend-ui

go 1.26.3

replace github.com/webview/webview_go => ./third_party/webview_go

require (
	github.com/a-h/templ v0.3.1020
	github.com/dusto/tend v0.1.1-0.20260828131543-7f8d4a9cb0ff
	github.com/webview/webview_go v0.0.0-00010101000000-000000000000
)

require (
	github.com/santhosh-tekuri/jsonschema/v6 v6.0.2 // indirect
	golang.org/x/text v0.14.0 // indirect
)

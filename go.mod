module github.com/dusto/tend-ui

go 1.26.3

replace github.com/webview/webview_go => ./third_party/webview_go

require (
	github.com/a-h/templ v0.3.1020
	github.com/dusto/tend v0.1.1-0.20260830214002-f601dfb9103f
	github.com/microcosm-cc/bluemonday v1.0.27
	github.com/webview/webview_go v0.0.0-00010101000000-000000000000
	github.com/yuin/goldmark v1.8.5
)

require (
	github.com/aymerick/douceur v0.2.0 // indirect
	github.com/gorilla/css v1.0.1 // indirect
	github.com/santhosh-tekuri/jsonschema/v6 v6.0.2 // indirect
	golang.org/x/net v0.51.0 // indirect
	golang.org/x/text v0.34.0 // indirect
)

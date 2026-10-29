#!/bin/sh
APP="BBGO.app"
APP_DIR=build/$APP

# cmd/bbgo-desktop was removed upstream; cmd/bbgo-lorca is the only GUI entry
# left. Same for cmd/bbgo-webview, which is why the Makefile defaults to lorca.
go build -o $APP_DIR/Contents/MacOS/bbgo-desktop ./cmd/bbgo-lorca

cat > $APP_DIR/Contents/Info.plist << EOF
<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
	<key>CFBundleExecutable</key>
	<string>bbgo-desktop</string>
	<key>CFBundleIconFile</key>
	<string>icon.icns</string>
	<key>CFBundleIdentifier</key>
	<string>com.bbgo.lorca</string>
</dict>
</plist>
EOF

cp -v desktop/icons/icon.icns $APP_DIR/Contents/Resources/icon.icns
find $APP_DIR

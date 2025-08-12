# Samar
A map for cameras. You can easily place cameras on map and then look at their information. Images, credentials and so on. Comments can arrive on hold camera icon on the map. You can add images or cameras themselves.
# Usage
Download the latest go version from official site.
In project root folder run:
`go build -o app main.go`
Add rights for usage 443 port via `sudo setcap 'cap_net_bind_service=+ep' ./app`
Create necessary directories (database, data, logs) and just run the app with `./app`
That's all.
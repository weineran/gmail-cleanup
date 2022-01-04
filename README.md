# gmail-cleanup
CLI tool for deleting attachments from gmail messages

# Quickstart
* Create GCP OAuth client ID credentials following these instructions: <a href="https://developers.google.com/workspace/guides/create-credentials#oauth-client-id" target="_blank">Create OAuth client ID credentials</a>
* Download the credentials as `credentials.json` to the repo directory.
* Install depedencies: 
```
go get google.golang.org/api/gmail/v1
go get golang.org/x/oauth2/google
```
* Give it a _go_ :p
```
go run remove-attachments.go 'size:10000000'
```

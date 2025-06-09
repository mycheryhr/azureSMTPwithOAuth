# azureSMTPwithOAuth

SMTP relay service that recieve emials from SMTP clients and send them to Office 365 using OAuth2 authentication/graph API.

**[DOWNLOAD](https://github.com/mmalcek/azureSMTPwithOAuth/releases/latest)** latest prebuid release (Windows, MAC, Linux)

## Motivation

- From September 2025, Microsoft will require all SMTP clients to use OAuth2 authentication for sending emails to Office 365. This service provides a simple way to relay emails from SMTP clients to Office 365 using OAuth2 authentication.
- This is useful for applications that need to send emails but do not support OAuth2 authentication natively, such as legacy applications or custom SMTP clients.
- I've created this application for our ([systems@work](https://systemsatwork.com)) internal use , but we decided to share it with the community as it may be useful for others as well.

**If you like this app you can buy me a coffe ;)**

[![Ko-fi](https://ko-fi.com/img/githubbutton_sm.svg)](https://ko-fi.com/mmalcek)

## Features

- SMTP relay service
- OAuth2 authentication
- Graph API integration
- Token cache and renewal. Tokens are stored in memory and renewed automatically.
- Supports multiple SMTP clients
- Works also with "Exchange Online Kiosk" plan which does not support SMTP oAuth authentication (thanks to graph API)

## Important

- This is SMTP relay ONLY! (No IMAP/POP3 support)
- This is not a full email server, it does not store emails, it only relays them to Office 365.
- SMTP Encryption (StartTLS, TLS) **is not supported!** It is highly recommended run this service on the same machine as your SMTP client. And setup `listen_addr:127.0.0.1:XXX`

## Quick Step By Step Summary

1. Register an application in Azure Entra ID (Azure AD) and configure it for OAuth2 authentication.
2. Update `config.yaml` with your Azure App Client ID, Client Secret, and Tenant ID.
3. Optionally encrypt config file (Windows only).
4. Install the service using the command line.
5. Start the service.
6. Configure your SMTP client to use the service as a relay.

More detailed instructions are provided below.

## Setup Entra ID (Azure AD) Application

- See quick guide **azureSMTPwithOAuth_RegisterApp.docx**

## Config file

```yaml
log: ""
log_level: debug
listen_addr: 127.0.0.1:2526
oauth2_config:
  client_id: AzureAppClientID
  client_secret: AzureAppClientSecret
  tenant_id: AzureTenantID
  scopes:
    - https://graph.microsoft.com/.default
fallback_smtp_user:
fallback_smtp_pass:
save_to_sent: false
```

- `log`: Path to log file. If empty, logs will be printed to stdout.
- `log_level`: Log level. Can be `debug`, `info`, `warn`, `error`.
- `listen_addr`: Address to listen on. Default is `127.0.0.1:2526`.
- `oauth2_config`: OAuth2 configuration.
  - `client_id`: Azure App Client ID.
  - `client_secret`: Azure App Client Secret.
  - `tenant_id`: Azure Tenant ID.
  - `scopes`: Scopes to request. Default is `https://graph.microsoft.com/.default`.
- `fallback_smtp_user`: Fallback SMTP user. If set, this user will be used if the SMTP client does not provide a user.
- `fallback_smtp_pass`: Fallback SMTP password. If set, this password will be used if the SMTP client does not provide a password.
- `save_to_sent`: If true, the service will save a copy of the sent email to the "Sent Items" folder in Office 365. Default is `false`.

## Usage

### Run from command line

- If you just start application from command line without any arguments, it will run as a console application and if config.yaml: `log: ""` is empty you can watch logs in console.

### Setup as service

- `.\azureSMTPwithOAuth.exe -service install`: Install the service.
- `.\azureSMTPwithOAuth.exe -service start`: Start the service.
- `.\azureSMTPwithOAuth.exe -service stop`: Stop the service.
- `.\azureSMTPwithOAuth.exe -service uninstall`: Uninstall the service.

### Other commands

- `.\azureSMTPwithOAuth.exe -encrypt`: Encrypt sensitive information in config file using DPAPI. Windows only.

### Configure SMTP Client/your application

- Set the SMTP server to the address and port specified in `listen_addr` (default is `127.0.0.1:2526`).
- StartTLS is not supported, so ensure your SMTP client is configured to connect without encryption.
- If client provides a username and password, they will be used for authentication. If not, the `fallback_smtp_user` and password will be used.

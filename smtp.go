package main

import (
	"bufio"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net"
	"net/mail"
	"strings"
	"sync"
	"time"

	"mime"
	"mime/multipart"
	"mime/quotedprintable"

	"net/http"

	"golang.org/x/oauth2"
)

// TokenCache holds cached OAuth2 tokens per user (thread-safe)
var TokenCache sync.Map

type cachedToken struct {
	token     string
	expiresAt time.Time
}

func handleSMTPConnection(conn net.Conn) {
	defer conn.Close()
	reader := bufio.NewReader(conn)
	writer := bufio.NewWriter(conn)
	fmt.Fprintf(writer, "220 SMTP Relay Ready\r\n")
	writer.Flush()

	var username, password string
	authenticated := false
	var mailFrom string
	var rcptTo []string
	var dataLines []string
	for {
		line, err := reader.ReadString('\n')
		if err != nil {
			logger.Error("Client read error", "error", err)
			fmt.Fprintf(writer, "421 4.7.0 Service not available\r\n")
			writer.Flush()
			return
		}
		line = strings.TrimRight(line, "\r\n")
		if line == "" { // Ignore empty lines
			continue
		}
		// Log the received command
		logger.Debug("Received SMTP command", "command", line)
		// Handle EHLO/HELO commands
		if strings.HasPrefix(strings.ToUpper(line), "EHLO") || strings.HasPrefix(strings.ToUpper(line), "HELO") {
			fmt.Fprintf(writer, "250-smtpRelay\r\n250-AUTH LOGIN\r\n250 STARTTLS\r\n")
			writer.Flush()
			continue
		}
		if strings.HasPrefix(strings.ToUpper(line), "AUTH LOGIN") {
			// Handle both: AUTH LOGIN (prompt for username) and AUTH LOGIN <base64-username>
			parts := strings.Fields(line)
			if len(parts) == 3 {
				// AUTH LOGIN <base64-username>
				userB64 := strings.TrimSpace(parts[2])
				username = decodeBase64(userB64)
				logger.Debug("AUTH LOGIN inline username", "username", username)
				fmt.Fprintf(writer, "334 UGFzc3dvcmQ6\r\n") // 'Password:' base64
				writer.Flush()
				passB64, _ := reader.ReadString('\n')
				passB64 = strings.TrimSpace(passB64)
				password = decodeBase64(passB64)
			} else {
				// Standard flow: prompt for username
				fmt.Fprintf(writer, "334 VXNlcm5hbWU6\r\n") // 'Username:' base64
				writer.Flush()
				userB64, _ := reader.ReadString('\n')
				userB64 = strings.TrimSpace(userB64)
				username = decodeBase64(userB64)
				logger.Debug("AUTH LOGIN username", "username", username)
				fmt.Fprintf(writer, "334 UGFzc3dvcmQ6\r\n") // 'Password:' base64
				writer.Flush()
				passB64, _ := reader.ReadString('\n')
				passB64 = strings.TrimSpace(passB64)
				password = decodeBase64(passB64)
			}
			if username == "" || password == "" {
				// Use fallback credentials from config if not provided by client
				if config.FallbackSMTPuser == "" || config.FallbackSMTPpass == "" {
					fmt.Fprintf(writer, "535 5.7.8 Authentication credentials invalid\r\n")
					writer.Flush()
					logger.Error("Authentication failed: no credentials provided")
					return
				}
				username = config.FallbackSMTPuser
				password = config.FallbackSMTPpass
			}
			// Validate username and password
			_, err = getCachedOAuth2Token(context.Background(), username, password)
			if err != nil {
				logger.Error("OAuth2 token retrieval failed", "error", err)
				writer.Flush()
				return
			}
			fmt.Fprintf(writer, "235 2.7.0 Authentication successful\r\n")
			writer.Flush()
			logger.Debug("User authenticated", "username", username)
			authenticated = true
			continue
		}
		// If not authenticated, any command other than AUTH should fail
		if !authenticated {
			logger.Error("Authentication required for command", "command", line)
			fmt.Fprintf(writer, "530 5.7.0 Authentication required\r\n")
			writer.Flush()
			continue
		}
		// Handle MAIL FROM, RCPT TO, DATA commands
		if strings.HasPrefix(strings.ToUpper(line), "MAIL FROM:") {
			mailFrom = extractAddress(line)
			fmt.Fprintf(writer, "250 2.1.0 Ok\r\n")
			writer.Flush()
			continue
		}
		if strings.HasPrefix(strings.ToUpper(line), "RCPT TO:") {
			addr := extractAddress(line)
			if addr != "" {
				rcptTo = append(rcptTo, addr)
			}
			fmt.Fprintf(writer, "250 2.1.5 Ok\r\n")
			writer.Flush()
			continue
		}
		if strings.HasPrefix(strings.ToUpper(line), "DATA") {
			fmt.Fprintf(writer, "354 End data with <CR><LF>.<CR><LF>\r\n")
			writer.Flush()
			dataLines = nil
			for {
				dataLine, err := reader.ReadString('\n')
				if err != nil {
					log.Printf("Client read error (DATA): %v", err)
					return
				}
				if strings.TrimSpace(dataLine) == "." {
					break
				}
				dataLines = append(dataLines, dataLine)
			}

			// Reconstruct message and normalize line endings for MIME parsing
			msg := strings.Join(dataLines, "")
			msg = strings.ReplaceAll(msg, "\r\n", "\n")
			msg = strings.ReplaceAll(msg, "\r", "\n")
			msg = strings.ReplaceAll(msg, "\n", "\r\n")

			// Parse subject, body, and attachments
			subject, body, isHTML, attachments, parseErr := parseSubjectBodyAndAttachments(msg)
			if parseErr != nil {
				fmt.Fprintf(writer, "550 5.6.0 Message parsing failed: %v\r\n", parseErr)
				writer.Flush()
				logger.Error("MIME parsing failed", "error", parseErr)
				return
			}

			// Get OAuth2 token and send via Graph API
			token, err := getCachedOAuth2Token(context.Background(), username, password)
			if err != nil {
				fmt.Fprintf(writer, "451 4.7.0 Temporary authentication failure\r\n")
				writer.Flush()
				logger.Error("Failed to get OAuth2 token", "error", err, "username", username)
				return
			}
			if err := sendMailGraphAPI(token, username, mailFrom, rcptTo, subject, body, isHTML, attachments); err != nil {
				fmt.Fprintf(writer, "550 5.7.0 Delivery failed: %v\r\n", err)
				writer.Flush()
				logger.Error("Failed to send email via Graph API", "error", err, "username", username, "mailFrom", mailFrom, "rcptTo", rcptTo)
				return
			}
			fmt.Fprintf(writer, "250 2.0.0 Ok: queued as graphapi\r\n")
			writer.Flush()
			// Reset for next message
			logger.Info("E-mail sent successfully", "username", username, "mailFrom", mailFrom, "rcptTo", rcptTo, "subject", subject)
			mailFrom = ""
			rcptTo = nil
			dataLines = nil
			continue
		}
		if strings.HasPrefix(strings.ToUpper(line), "QUIT") {
			fmt.Fprintf(writer, "221 2.0.0 Bye\r\n")
			writer.Flush()
			return
		}
		// Default: 502 Command not implemented
		fmt.Fprintf(writer, "502 5.5.2 Command not implemented\r\n")
		writer.Flush()
	}
}

// extractAddress extracts the email address from SMTP command line
func extractAddress(line string) string {
	start := strings.Index(line, "<")
	end := strings.Index(line, ">")
	if start != -1 && end != -1 && end > start {
		return line[start+1 : end]
	}
	// fallback: try after colon
	parts := strings.SplitN(line, ":", 2)
	if len(parts) == 2 {
		return strings.TrimSpace(parts[1])
	}
	return ""
}

// Attachment represents a parsed email attachment
// filename, contentType, and base64-encoded content
type Attachment struct {
	Filename    string
	ContentType string
	Content     string // base64-encoded
}

// parseSubjectBodyAndAttachments parses the subject, body, and attachments from a raw SMTP message
func parseSubjectBodyAndAttachments(msg string) (subject, body string, isHTML bool, attachments []Attachment, err error) {
	// Ensure message ends with a newline for robust parsing
	if !strings.HasSuffix(msg, "\n") {
		msg += "\n"
	}
	r := strings.NewReader(msg)
	m, err := mail.ReadMessage(r)
	if err != nil {
		return "", "", false, nil, fmt.Errorf("mail.ReadMessage failed: %w", err)
	}
	wd := new(mime.WordDecoder)
	subjectRaw := m.Header.Get("Subject")
	subject, err = wd.DecodeHeader(subjectRaw)
	if err != nil {
		subject = subjectRaw // fallback to raw if decode fails
	}
	ct := m.Header.Get("Content-Type")
	cte := strings.ToLower(m.Header.Get("Content-Transfer-Encoding"))
	if strings.Contains(strings.ToLower(ct), "html") {
		isHTML = true
	}
	mediaType, params, err := mime.ParseMediaType(ct)
	dataContent := []byte{}
	if err == nil && strings.HasPrefix(mediaType, "multipart/") {
		mr := multipart.NewReader(m.Body, params["boundary"])
		for {
			p, err := mr.NextPart()
			if err == io.EOF {
				break
			}
			if err != nil {
				continue
			}
			// Decode the part's subject if available
			if strings.HasPrefix(p.Header.Get("Content-Disposition"), "attachment") {
				filename := p.FileName()
				// Try to extract filename from Content-Type if still empty
				if filename == "" {
					ct := p.Header.Get("Content-Type")
					_, params, err := mime.ParseMediaType(ct)
					if err == nil {
						if n, ok := params["name"]; ok && n != "" {
							filename = n
							logger.Debug("Attachment filename extracted from Content-Type name param", "filename", filename)
						}
					}
				}
				ctype := p.Header.Get("Content-Type")
				if ctype == "" {
					ctype = "application/octet-stream"
				}
				attCTE := strings.ToLower(p.Header.Get("Content-Transfer-Encoding"))
				if dataContent, err = decodeMessage(attCTE, p); err != nil {
					logger.Warn("Failed to decode attachment, skipping", "filename", filename, "error", err)
					continue // skip this attachment if decoding fails
				}
				if filename == "" || ctype == "" || len(dataContent) == 0 {
					logger.Warn("Invalid attachment detected, skipping", "filename", filename, "contentType", ctype, "dataLength", len(dataContent))
					continue // skip invalid attachments
				}
				attachments = append(attachments, Attachment{
					Filename:    filename,
					ContentType: ctype,
					Content:     base64.StdEncoding.EncodeToString(dataContent),
				})
			} else {
				// treat as body part
				cte := strings.ToLower(p.Header.Get("Content-Transfer-Encoding"))
				if dataContent, err = decodeMessage(cte, p); err != nil {
					log.Printf("Failed to decode body part: %v", err)
					continue // skip this part if decoding fails
				}
				// If the part is HTML, set isHTML flag
				if strings.Contains(strings.ToLower(p.Header.Get("Content-Type")), "html") {
					isHTML = true
				}
				body = string(dataContent)
			}
		}
		return subject, body, isHTML, attachments, nil
	}
	// Not multipart: fallback to old logic
	if dataContent, err = decodeMessage(cte, m.Body); err != nil {
		return "", "", false, nil, fmt.Errorf("failed to decode message body: %w", err)
	}

	// log.Printf("[MIME PARSE] subject=%q, body-len=%d, attachments=%d (not multipart)", subject, len(body), len(attachments))
	return subject, string(dataContent), isHTML, nil, nil
}

func decodeMessage(c string, r io.Reader) (content []byte, err error) {
	switch c {
	case "base64":
		content, err = io.ReadAll(base64.NewDecoder(base64.StdEncoding, r))
	case "quoted-printable":
		content, err = io.ReadAll(quotedprintable.NewReader(r))
	default:
		content, err = io.ReadAll(r)
	}
	if err != nil {
		return nil, fmt.Errorf("failed to read part: %w", err)
	}
	return content, nil
}

// sendMailGraphAPI sends the email via Microsoft Graph API /sendMail
func sendMailGraphAPI(token, sender, mailFrom string, rcptTo []string, subject, body string, isHTML bool, attachments []Attachment) error {
	url := "https://microsoftgraph.chinacloudapi.cn/v1.0/users/" + sender + "/sendMail"
	contentType := "html"
	// contentType := "text"
	// if isHTML {
	// 	contentType = "html"
	// }
	var toRecipients []map[string]map[string]string
	for _, addr := range rcptTo {
		toRecipients = append(toRecipients, map[string]map[string]string{
			"emailAddress": {"address": addr},
		})
	}
	var graphAttachments []map[string]interface{}
	for _, att := range attachments {
		graphAttachments = append(graphAttachments, map[string]interface{}{
			"@odata.type":  "#microsoft.graph.fileAttachment",
			"name":         att.Filename,
			"contentType":  att.ContentType,
			"contentBytes": att.Content,
		})
	}
	if graphAttachments == nil {
		graphAttachments = make([]map[string]interface{}, 0)
	}
	msg := map[string]interface{}{
		"message": map[string]interface{}{
			"subject": subject,
			"body": map[string]string{
				"contentType": contentType,
				"content":     body,
			},
			"toRecipients": toRecipients,
			"from": map[string]map[string]string{
				"emailAddress": {"address": mailFrom},
			},
			"attachments": graphAttachments,
		},
		"saveToSentItems": config.SaveToSent,
	}
	jsonBody, _ := json.Marshal(msg)
	request, err := http.NewRequest("POST", url, strings.NewReader(string(jsonBody)))
	if err != nil {
		return err
	}
	request.Header.Set("Authorization", "Bearer "+token)
	request.Header.Set("Content-Type", "application/json")
	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(request)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		b, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("Graph API error: %s", string(b))
	}
	return nil
}

func decodeBase64(s string) string {
	b, err := base64.StdEncoding.DecodeString(s)
	if err != nil {
		return ""
	}
	return string(b)
}

// getCachedOAuth2Token returns a cached token or fetches a new one if expired
func getCachedOAuth2Token(ctx context.Context, username, password string) (string, error) {
	if val, ok := TokenCache.Load(username); ok {
		tok := val.(cachedToken)
		if time.Now().Before(tok.expiresAt) {
			logger.Debug("Using cached OAuth2 token", "username", username, "expires_at", tok.expiresAt)
			return tok.token, nil
		}
	}
	token, expiresIn, err := getOAuth2TokenWithExpiry(ctx, username, password)
	if err != nil {
		return "", err
	}
	TokenCache.Store(username, cachedToken{
		token:     token,
		expiresAt: time.Now().Add(time.Duration(expiresIn-60) * time.Second), // refresh 1 min before expiry
	})
	logger.Debug("New OAuth2 token cached", "username", username, "expires_in", expiresIn)
	return token, nil
}

// getOAuth2TokenWithExpiry returns token and expiry (in seconds)
func getOAuth2TokenWithExpiry(ctx context.Context, username, password string) (string, int, error) {
	tokenURL := fmt.Sprintf("https://login.partner.microsoftonline.cn/%s/oauth2/v2.0/token", config.OAuth2Config.TenantID)
	params := make(map[string][]string)
	params["client_id"] = []string{config.OAuth2Config.ClientID}
	params["scope"] = []string{strings.Join(config.OAuth2Config.Scopes, " ")}
	params["username"] = []string{username}
	params["password"] = []string{password}
	params["grant_type"] = []string{"password"}
	params["client_secret"] = []string{config.OAuth2Config.ClientSecret}

	resp, err := oauth2.NewClient(ctx, nil).PostForm(tokenURL, params)
	if err != nil {
		return "", 0, err
	}
	defer resp.Body.Close()
	var result struct {
		AccessToken string `json:"access_token"`
		ExpiresIn   int    `json:"expires_in"`
	}
	// Debug: print the raw response body for troubleshooting
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", 0, fmt.Errorf("failed to read token response: %v", err)
	}
	if err := json.Unmarshal(body, &result); err != nil {
		return "", 0, fmt.Errorf("failed to parse token response: %v, body: %s", err, string(body))
	}
	// Check if access token is present
	if result.AccessToken == "" {
		return "", 0, fmt.Errorf("no access token in response, body: %s", string(body))
	}
	logger.Debug("OAuth2 token retrieved", "username", username, "expires_in", result.ExpiresIn)
	return result.AccessToken, result.ExpiresIn, nil
}

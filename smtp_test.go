package main

import (
	"bytes"
	"encoding/base64"
	"mime/multipart"
	"strings"
	"testing"
)

func TestDecodeMessage_Base64(t *testing.T) {
	input := base64.StdEncoding.EncodeToString([]byte("hello world"))
	decoded, err := decodeMessage("base64", strings.NewReader(input))
	if err != nil {
		t.Fatalf("decodeMessage base64 failed: %v", err)
	}
	if string(decoded) != "hello world" {
		t.Errorf("expected 'hello world', got '%s'", string(decoded))
	}
}

func TestDecodeMessage_QuotedPrintable(t *testing.T) {
	input := "hello=20world=21"
	decoded, err := decodeMessage("quoted-printable", strings.NewReader(input))
	if err != nil {
		t.Fatalf("decodeMessage quoted-printable failed: %v", err)
	}
	if string(decoded) != "hello world!" {
		t.Errorf("expected 'hello world!', got '%s'", string(decoded))
	}
}

func TestDecodeMessage_Default(t *testing.T) {
	input := "plain text"
	decoded, err := decodeMessage("", strings.NewReader(input))
	if err != nil {
		t.Fatalf("decodeMessage default failed: %v", err)
	}
	if string(decoded) != input {
		t.Errorf("expected '%s', got '%s'", input, string(decoded))
	}
}

func TestParseSubjectBodyAndAttachments_Simple(t *testing.T) {
	raw := "From: test@example.com\r\nTo: you@example.com\r\nSubject: Hello\r\n\r\nThis is the body."
	subject, body, isHTML, attachments, err := parseSubjectBodyAndAttachments(raw)
	if err != nil {
		t.Fatalf("parseSubjectBodyAndAttachments failed: %v", err)
	}
	if subject != "Hello" {
		t.Errorf("expected subject 'Hello', got '%s'", subject)
	}
	// Accept both with and without trailing newline
	expectedBody := "This is the body."
	if strings.TrimRight(body, "\r\n") != expectedBody {
		t.Errorf("expected body '%s', got '%s'", expectedBody, body)
	}
	if isHTML {
		t.Errorf("expected isHTML false, got true")
	}
	if len(attachments) != 0 {
		t.Errorf("expected 0 attachments, got %d", len(attachments))
	}
}

func TestParseSubjectBodyAndAttachments_SimpleHTML(t *testing.T) {
	raw := "From: test@example.com\r\nTo: you@example.com\r\nSubject: Hello\r\nContent-Type: text/html\r\n\r\n<html><body>Hi!</body></html>"
	subject, body, isHTML, attachments, err := parseSubjectBodyAndAttachments(raw)
	if err != nil {
		t.Fatalf("parseSubjectBodyAndAttachments failed: %v", err)
	}
	if subject != "Hello" {
		t.Errorf("expected subject 'Hello', got '%s'", subject)
	}
	expectedBody := "<html><body>Hi!</body></html>"
	if strings.TrimRight(body, "\r\n") != expectedBody {
		t.Errorf("expected body '%s', got '%s'", expectedBody, body)
	}
	if !isHTML {
		t.Errorf("expected isHTML true, got false")
	}
	if len(attachments) != 0 {
		t.Errorf("expected 0 attachments, got %d", len(attachments))
	}
}

func TestParseSubjectBodyAndAttachments_MultipartWithAttachment(t *testing.T) {
	var buf bytes.Buffer
	w := multipart.NewWriter(&buf)
	boundary := w.Boundary()
	// Body part
	bodyPart, _ := w.CreatePart(map[string][]string{
		"Content-Type":              {"text/plain"},
		"Content-Transfer-Encoding": {"7bit"},
	})
	bodyPart.Write([]byte("This is the body."))
	// Attachment part
	attPart, _ := w.CreatePart(map[string][]string{
		"Content-Type":              {"text/plain; name=\"file.txt\""},
		"Content-Disposition":       {"attachment; filename=\"file.txt\""},
		"Content-Transfer-Encoding": {"base64"},
	})
	attContent := base64.StdEncoding.EncodeToString([]byte("file content"))
	attPart.Write([]byte(attContent))
	w.Close()
	msg := "From: test@example.com\r\nTo: you@example.com\r\nSubject: Multipart\r\nMIME-Version: 1.0\r\nContent-Type: multipart/mixed; boundary=\"" + boundary + "\"\r\n\r\n" + buf.String()
	subject, body, isHTML, attachments, err := parseSubjectBodyAndAttachments(msg)
	if err != nil {
		t.Fatalf("parseSubjectBodyAndAttachments failed: %v", err)
	}
	if subject != "Multipart" {
		t.Errorf("expected subject 'Multipart', got '%s'", subject)
	}
	if strings.TrimRight(body, "\r\n") != "This is the body." {
		t.Errorf("expected body 'This is the body.', got '%s'", body)
	}
	if isHTML {
		t.Errorf("expected isHTML false, got true")
	}
	if len(attachments) != 1 {
		t.Errorf("expected 1 attachment, got %d", len(attachments))
	}
	if attachments[0].Filename != "file.txt" {
		t.Errorf("expected attachment filename 'file.txt', got '%s'", attachments[0].Filename)
	}
	decoded, _ := base64.StdEncoding.DecodeString(attachments[0].Content)
	if string(decoded) != "file content" {
		t.Errorf("expected attachment content 'file content', got '%s'", string(decoded))
	}
}

func TestParseSubjectBodyAndAttachments_MultipartHTMLBody(t *testing.T) {
	var buf bytes.Buffer
	w := multipart.NewWriter(&buf)
	boundary := w.Boundary()
	// HTML body part
	bodyPart, _ := w.CreatePart(map[string][]string{
		"Content-Type":              {"text/html"},
		"Content-Transfer-Encoding": {"7bit"},
	})
	bodyPart.Write([]byte("<b>HTML Body</b>"))
	w.Close()
	msg := "From: test@example.com\r\nTo: you@example.com\r\nSubject: HTMLMultipart\r\nMIME-Version: 1.0\r\nContent-Type: multipart/alternative; boundary=\"" + boundary + "\"\r\n\r\n" + buf.String()
	subject, body, isHTML, attachments, err := parseSubjectBodyAndAttachments(msg)
	if err != nil {
		t.Fatalf("parseSubjectBodyAndAttachments failed: %v", err)
	}
	if subject != "HTMLMultipart" {
		t.Errorf("expected subject 'HTMLMultipart', got '%s'", subject)
	}
	if strings.TrimRight(body, "\r\n") != "<b>HTML Body</b>" {
		t.Errorf("expected body '<b>HTML Body</b>', got '%s'", body)
	}
	if !isHTML {
		t.Errorf("expected isHTML true, got false")
	}
	if len(attachments) != 0 {
		t.Errorf("expected 0 attachments, got %d", len(attachments))
	}
}

func TestParseSubjectBodyAndAttachments_MultipartNoBody(t *testing.T) {
	var buf bytes.Buffer
	w := multipart.NewWriter(&buf)
	boundary := w.Boundary()
	// Only attachment, no body part
	attPart, _ := w.CreatePart(map[string][]string{
		"Content-Type":              {"application/octet-stream; name=\"file.bin\""},
		"Content-Disposition":       {"attachment; filename=\"file.bin\""},
		"Content-Transfer-Encoding": {"base64"},
	})
	attContent := base64.StdEncoding.EncodeToString([]byte("binarydata"))
	attPart.Write([]byte(attContent))
	w.Close()
	msg := "From: test@example.com\r\nTo: you@example.com\r\nSubject: OnlyAttachment\r\nMIME-Version: 1.0\r\nContent-Type: multipart/mixed; boundary=\"" + boundary + "\"\r\n\r\n" + buf.String()
	subject, body, isHTML, attachments, err := parseSubjectBodyAndAttachments(msg)
	if err != nil {
		t.Fatalf("parseSubjectBodyAndAttachments failed: %v", err)
	}
	if subject != "OnlyAttachment" {
		t.Errorf("expected subject 'OnlyAttachment', got '%s'", subject)
	}
	if body != "" {
		t.Errorf("expected empty body, got '%s'", body)
	}
	if isHTML {
		t.Errorf("expected isHTML false, got true")
	}
	if len(attachments) != 1 {
		t.Errorf("expected 1 attachment, got %d", len(attachments))
	}
	if attachments[0].Filename != "file.bin" {
		t.Errorf("expected attachment filename 'file.bin', got '%s'", attachments[0].Filename)
	}
	decoded, _ := base64.StdEncoding.DecodeString(attachments[0].Content)
	if string(decoded) != "binarydata" {
		t.Errorf("expected attachment content 'binarydata', got '%s'", string(decoded))
	}
}

func TestParseSubjectBodyAndAttachments_EncodedSubject(t *testing.T) {
	raw := "From: test@example.com\r\nTo: you@example.com\r\nSubject: =?UTF-8?B?SGVsbG8g8J+agA==?=\r\n\r\nBody"
	subject, body, _, _, err := parseSubjectBodyAndAttachments(raw)
	if err != nil {
		t.Fatalf("parseSubjectBodyAndAttachments failed: %v", err)
	}
	if subject != "Hello 🚀" {
		t.Errorf("expected subject 'Hello 🚀', got '%s'", subject)
	}
	if strings.TrimRight(body, "\r\n") != "Body" {
		t.Errorf("expected body 'Body', got '%s'", body)
	}
}

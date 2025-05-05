package main

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/csv"
	"fmt"
	"io"
	"log"
	"mime/multipart"
	"net/http"
	"net/textproto"
	"os"
	"path/filepath"
	"strings"

	"github.com/aymerick/raymond"
	"github.com/gin-gonic/gin"
	"github.com/sirupsen/logrus"
	"golang.org/x/oauth2"
	"golang.org/x/oauth2/google"
	"google.golang.org/api/gmail/v1"
	"google.golang.org/api/option"
)

func main() {
	router := gin.Default()
	router.LoadHTMLGlob("views/*")
	router.Static("/static", "./static")

	router.GET("/", showIndex)
	router.POST("/upload-template", uploadTemplate)
	router.POST("/upload-csv", uploadCSV)
	router.POST("/send-emails", sendEmails)

	router.Run(":8080")
}

func showErrorPage(c *gin.Context, message string, statusCode int) {
	c.HTML(statusCode, "error.html", gin.H{
		"Message": message,
	})
}

func showIndex(c *gin.Context) {
	templates, _ := filepath.Glob("templates/*.html")
	for i, t := range templates {
		templates[i] = filepath.Base(t)
	}
	c.HTML(http.StatusOK, "index.html", gin.H{
		"Templates": templates,
	})
}

func uploadTemplate(c *gin.Context) {
	file, err := c.FormFile("template")
	if err != nil {
		showErrorPage(c, fmt.Sprintf("Template upload error: %v", err), http.StatusBadRequest)
		return
	}
	c.SaveUploadedFile(file, filepath.Join("templates", file.Filename))
	c.Redirect(http.StatusSeeOther, "/")
}

func uploadCSV(c *gin.Context) {
	file, err := c.FormFile("csv")
	if err != nil {
		showErrorPage(c, fmt.Sprintf("CSV upload error: %v", err), http.StatusBadRequest)
		return
	}
	c.SaveUploadedFile(file, filepath.Join("uploads", file.Filename))
	c.Redirect(http.StatusSeeOther, "/")
}

func sendEmails(c *gin.Context) {
	templateName := c.PostForm("template")
	csvFile := c.PostForm("csvfile")

	file, err := os.Open(filepath.Join("uploads", csvFile))
	if err != nil {
		logrus.Errorf("Error opening CSV file: %v", err)
		showErrorPage(c, fmt.Sprintf("Error opening CSV file: %v", err), http.StatusInternalServerError)
		return
	}
	defer file.Close()

	reader := csv.NewReader(file)
	headers, err := reader.Read()
	if err != nil {
		showErrorPage(c, fmt.Sprintf("Error reading CSV headers: %v", err), http.StatusInternalServerError)
		return
	}

	for {
		record, err := reader.Read()
		if err == io.EOF {
			break
		}
		if err != nil {
			logrus.Errorf("Error reading CSV record: %v", err)
			continue // Log the error and continue processing other rows
		}

		data := make(map[string]interface{})
		for i, header := range headers {
			data[header] = record[i]
		}

		email, ok := data["email"].(string)
		if !ok {
			logrus.Errorf("Invalid or missing 'email' field in CSV record: %v", record)
			continue // Skip this record if email is invalid
		}
		subject := "Your Subject Here"
		body := parseTemplate(templateName, data)

		// Call your existing SendEmailWithGCP function
		if err := SendEmailWithGCP(email, subject, body, data); err != nil {
			logrus.Errorf("Error sending email to %s: %v", email, err)
			showErrorPage(c, fmt.Sprintf("Error sending email to %s: %v", email, err), http.StatusInternalServerError)
			return // Stop sending further emails if one fails critically
		}
	}

	c.String(http.StatusOK, "Emails sent successfully.")
}

const (
	awsRegion = "eu-north-1"
)

func SendEmailWithGCP(email, subject, temp string, variables map[string]interface{}) error {
	// Create an SES session using the provided credentials

	ctx := context.Background()

	clientID := os.Getenv("GCP_SMS_CLIENT_ID")
	clientSecret := os.Getenv("GCP_SMS_CLIENT_SECRET")
	redirectURI := os.Getenv("GCP_SMS_REDIRECT_URI")
	refreshToken := os.Getenv("GCP_SMS_REFRESH_TOKEN")
	config := &oauth2.Config{
		ClientID:     clientID,
		ClientSecret: clientSecret,
		RedirectURL:  redirectURI,
		Scopes:       []string{gmail.GmailSendScope},
		Endpoint:     google.Endpoint,
	}

	token := &oauth2.Token{RefreshToken: refreshToken}
	tokenSource := config.TokenSource(ctx, token)
	client := oauth2.NewClient(ctx, tokenSource)

	srv, err := gmail.NewService(ctx, option.WithHTTPClient(client))
	if err != nil {
		log.Fatalf("Unable to create Gmail service: %v", err)
		return err
	}

	header := make(map[string]interface{})
	header["To"] = email
	header["From"] = "no-reply@renda.co"
	header["Subject"] = subject
	header["Content-Type"] = "text/html; charset=\"utf-8\""

	//header["Content-Type"] = htmlTemplate

	var msg bytes.Buffer
	for k, v := range header {
		msg.WriteString(fmt.Sprintf("%s: %s\r\n", k, v))
	}
	msg.WriteString("\r\n" + parseTemplate(temp, variables))

	rawMessage := base64.URLEncoding.EncodeToString(msg.Bytes())

	// Create the Gmail API message
	message := &gmail.Message{
		Raw: rawMessage,
	}
	_, err = srv.Users.Messages.Send("me", message).Do()
	if err != nil {
		log.Fatal("Failed to send email:", err)
		return err
	}

	return nil
}

func SendEmailWithGCPWithFile(email, subject, temp string, variables map[string]interface{}, file multipart.File, fileName string) error {
	fmt.Println("in email with file")
	ctx := context.Background()

	clientID := os.Getenv("GCP_SMS_CLIENT_ID")
	clientSecret := os.Getenv("GCP_SMS_CLIENT_SECRET")
	redirectURI := os.Getenv("GCP_SMS_REDIRECT_URI")
	refreshToken := os.Getenv("GCP_SMS_REFRESH_TOKEN")
	config := &oauth2.Config{
		ClientID:     clientID,
		ClientSecret: clientSecret,
		RedirectURL:  redirectURI,
		Scopes:       []string{gmail.GmailSendScope},
		Endpoint:     google.Endpoint,
	}

	token := &oauth2.Token{RefreshToken: refreshToken}
	tokenSource := config.TokenSource(ctx, token)
	client := oauth2.NewClient(ctx, tokenSource)

	srv, err := gmail.NewService(ctx, option.WithHTTPClient(client))
	if err != nil {
		log.Fatalf("Unable to create Gmail service: %v", err)
		return err
	}

	var msg bytes.Buffer
	writer := multipart.NewWriter(&msg)

	// Write headers manually
	fmt.Fprintf(&msg, "To: %s\r\n", email)
	fmt.Fprintf(&msg, "From: %s\r\n", "no-reply@renda.co")
	fmt.Fprintf(&msg, "Subject: %s\r\n", subject)
	fmt.Fprintf(&msg, "MIME-Version: 1.0\r\n")
	fmt.Fprintf(&msg, "Content-Type: multipart/mixed; boundary=%s\r\n", writer.Boundary())
	fmt.Fprintf(&msg, "\r\n")

	// Email body
	htmlPart, err := writer.CreatePart(textproto.MIMEHeader{
		"Content-Type": {"text/html; charset=UTF-8"},
	})
	if err != nil {
		return fmt.Errorf("error creating email body part: %v", err)
	}
	_, err = htmlPart.Write([]byte(parseTemplate(temp, variables)))
	if err != nil {
		return fmt.Errorf("error writing email body: %v", err)
	}

	// Attaching the file
	attachmentHeader := make(textproto.MIMEHeader)
	attachmentHeader.Set("Content-Type", "application/octet-stream")
	attachmentHeader.Set("Content-Disposition", fmt.Sprintf(`attachment; filename="%s"`, fileName))

	attachmentPart, err := writer.CreatePart(attachmentHeader)
	if err != nil {
		return fmt.Errorf("error creating attachment part: %v", err)
	}

	// Copy the file into the attachment part
	if _, err := io.Copy(attachmentPart, file); err != nil {
		return fmt.Errorf("error copying file to attachment: %v", err)
	}

	// Close the multipart writer
	err = writer.Close()
	if err != nil {
		return fmt.Errorf("error closing multipart writer: %v", err)
	}

	// Encode the message as base64
	rawMessage := base64.URLEncoding.EncodeToString(msg.Bytes())

	// Create the Gmail API message
	message := &gmail.Message{
		Raw: rawMessage,
	}
	_, err = srv.Users.Messages.Send("me", message).Do()
	if err != nil {
		log.Fatal("Failed to send email:", err)
		return err
	}

	log.Println("Email sent successfully.")
	return nil
}

func parseTemplate(template string, data interface{}) string {
	// Assuming 'main.go' is run from project root
	templatesDir := "templates"

	// Construct the full path to the template file
	filePath := filepath.Join(templatesDir, template)

	tpl, err := os.ReadFile(filePath)
	if err != nil {
		logrus.Errorf("Template read error: %v", err)
		return ""
	}

	output := raymond.MustRender(string(tpl), data)
	return output
}

func SendEmailWithGCPWithFileAndCC(email string, ccEmails []string, subject, temp string, variables map[string]interface{}, file multipart.File, fileName string) error {
	fmt.Println("in email with file")
	ctx := context.Background()

	clientID := os.Getenv("GCP_SMS_CLIENT_ID")
	clientSecret := os.Getenv("GCP_SMS_CLIENT_SECRET")
	redirectURI := os.Getenv("GCP_SMS_REDIRECT_URI")
	refreshToken := os.Getenv("GCP_SMS_REFRESH_TOKEN")

	config := &oauth2.Config{
		ClientID:     clientID,
		ClientSecret: clientSecret,
		RedirectURL:  redirectURI,
		Scopes:       []string{gmail.GmailSendScope},
		Endpoint:     google.Endpoint,
	}

	token := &oauth2.Token{RefreshToken: refreshToken}
	tokenSource := config.TokenSource(ctx, token)
	client := oauth2.NewClient(ctx, tokenSource)

	srv, err := gmail.NewService(ctx, option.WithHTTPClient(client))
	if err != nil {
		log.Fatalf("Unable to create Gmail service: %v", err)
		return err
	}

	var msg bytes.Buffer
	writer := multipart.NewWriter(&msg)

	// Email Headers
	fmt.Fprintf(&msg, "To: %s\r\n", email)

	// Add CC Recipients if provided
	if len(ccEmails) > 0 {
		ccList := strings.Join(ccEmails, ", ")
		fmt.Fprintf(&msg, "Cc: %s\r\n", ccList)
	}

	fmt.Fprintf(&msg, "From: %s\r\n", "no-reply@renda.co")
	fmt.Fprintf(&msg, "Subject: %s\r\n", subject)
	fmt.Fprintf(&msg, "MIME-Version: 1.0\r\n")
	fmt.Fprintf(&msg, "Content-Type: multipart/mixed; boundary=%s\r\n", writer.Boundary())
	fmt.Fprintf(&msg, "\r\n")

	// Email body
	htmlPart, err := writer.CreatePart(textproto.MIMEHeader{
		"Content-Type": {"text/html; charset=UTF-8"},
	})
	if err != nil {
		return fmt.Errorf("error creating email body part: %v", err)
	}
	_, err = htmlPart.Write([]byte(parseTemplate(temp, variables)))
	if err != nil {
		return fmt.Errorf("error writing email body: %v", err)
	}

	// Attach the file
	attachmentHeader := make(textproto.MIMEHeader)
	attachmentHeader.Set("Content-Type", "application/octet-stream")
	attachmentHeader.Set("Content-Disposition", fmt.Sprintf(`attachment; filename="%s"`, fileName))

	attachmentPart, err := writer.CreatePart(attachmentHeader)
	if err != nil {
		return fmt.Errorf("error creating attachment part: %v", err)
	}

	if _, err := io.Copy(attachmentPart, file); err != nil {
		return fmt.Errorf("error copying file to attachment: %v", err)
	}

	// Close the writer
	err = writer.Close()
	if err != nil {
		return fmt.Errorf("error closing multipart writer: %v", err)
	}

	// Encode the message as base64
	rawMessage := base64.URLEncoding.EncodeToString(msg.Bytes())

	// Create the Gmail API message
	message := &gmail.Message{
		Raw: rawMessage,
	}
	_, err = srv.Users.Messages.Send("me", message).Do()
	if err != nil {
		log.Fatalf("Failed to send email: %v", err)
		return err
	}

	log.Println("Email sent successfully.")
	return nil
}

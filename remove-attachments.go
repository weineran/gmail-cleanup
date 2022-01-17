/**
 * @license
 * Copyright Google Inc.
 *
 * Licensed under the Apache License, Version 2.0 (the "License");
 * you may not use this file except in compliance with the License.
 * You may obtain a copy of the License at
 *
 *     https://www.apache.org/licenses/LICENSE-2.0
 *
 * Unless required by applicable law or agreed to in writing, software
 * distributed under the License is distributed on an "AS IS" BASIS,
 * WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
 * See the License for the specific language governing permissions and
 * limitations under the License.
 */
// [START gmail_quickstart]
package main

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	//"github.com/kylelemons/godebug/diff"
	"io/ioutil"
	"log"
	"mime/quotedprintable"
	"net/http"
	"os"
	"regexp"
	"sort"
	"strings"

	"golang.org/x/oauth2"
	"golang.org/x/oauth2/google"
	"google.golang.org/api/gmail/v1"
	"google.golang.org/api/option"
)

// Retrieve a token, saves the token, then returns the generated client.
func getClient(config *oauth2.Config) *http.Client {
	// The file token.json stores the user's access and refresh tokens, and is
	// created automatically when the authorization flow completes for the first
	// time.
	tokFile := "token.json"
	tok, err := tokenFromFile(tokFile)
	if err != nil {
		tok = getTokenFromWeb(config)
		saveToken(tokFile, tok)
	}
	return config.Client(context.Background(), tok)
}

// Request a token from the web, then returns the retrieved token.
func getTokenFromWeb(config *oauth2.Config) *oauth2.Token {
	authURL := config.AuthCodeURL("state-token", oauth2.AccessTypeOffline)
	fmt.Printf("Go to the following link in your browser then type the "+
		"authorization code: \n%v\n", authURL)

	var authCode string
	if _, err := fmt.Scan(&authCode); err != nil {
		log.Fatalf("Unable to read authorization code: %v", err)
	}

	tok, err := config.Exchange(context.TODO(), authCode)
	if err != nil {
		log.Fatalf("Unable to retrieve token from web: %v", err)
	}
	return tok
}

// Retrieves a token from a local file.
func tokenFromFile(file string) (*oauth2.Token, error) {
	f, err := os.Open(file)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	tok := &oauth2.Token{}
	err = json.NewDecoder(f).Decode(tok)
	return tok, err
}

// Saves a token to a file path.
func saveToken(path string, token *oauth2.Token) {
	fmt.Printf("Saving credential file to: %s\n", path)
	f, err := os.OpenFile(path, os.O_RDWR|os.O_CREATE|os.O_TRUNC, 0600)
	if err != nil {
		log.Fatalf("Unable to cache oauth token: %v", err)
	}
	defer f.Close()
	json.NewEncoder(f).Encode(token)
}

// See here why this is needed: https://stackoverflow.com/a/15621614
func convertToQuotedPrintable(s string) string {
	var b strings.Builder
	w := quotedprintable.NewWriter(&b)
	w.Write([]byte(s))
	w.Close()

	return b.String()
}

func convertPartToRawExAttachments(p *gmail.MessagePart, boundary string, depth int) string {
	var result string

	for _, header := range p.Headers {
		result = result + header.Name + ": " + header.Value + "\r\n"
	}

	if p.Filename == "" && p.Body != nil {
		result += "\r\n"
		decodedData, _ := base64.URLEncoding.DecodeString(p.Body.Data)
		decodedDataStr := convertToQuotedPrintable(string(decodedData))
		result += decodedDataStr
		result += "\r\n"
		result = result + "--" + boundary + "\r\n"
	}

	for _, subpart := range p.Parts {
		// recurse
		result += convertPartToRawExAttachments(subpart, boundary, depth+1)
	}

	// The last boundary has a trailing "--". See e.g. https://docs.microsoft.com/en-us/exchange/troubleshoot/administration/multipart-mixed-mime-message-format
	if depth == 0 {
		expectedSuffix := "\r\n"
		if !strings.HasSuffix(result, expectedSuffix) {
			log.Fatalf(`Expected suffix [%s] on result [%s]`, expectedSuffix, result)
		}

		newLength := len(result) - len(expectedSuffix)
		result = result[:newLength]

		result += "--"
	} else {
		if depth < 0 {
			log.Fatalf(`Recursion depth [%d] cannot be less than 0`, depth)
		}
	}

	return result
}

func readBoundaryTryAgain(h string) string {
	re := regexp.MustCompile(`boundary=([^\r\n]*)`)
	matches := re.FindSubmatch([]byte(h))
	if len(matches) > 2 {
		errorString := fmt.Sprintf("Found multiple matches for boundary [%q]", matches)
		panic(errorString)
	}
	if len(matches) <= 1 {
		errorString := fmt.Sprintf("Failed to find matches for boundary in header [%s]", h)
		panic(errorString)
	}

	boundary := string(matches[1])
	log.Printf("Found boundary on second try [%+v]\n", boundary)

	return boundary
}

func readBoundaryFromHeaders(headers []*gmail.MessagePartHeader) string {
	var boundary string

	for _, header := range headers {
		if strings.ToLower(header.Name) == "content-type" && strings.Contains(header.Value, `boundary=`) {
			if boundary != "" {
				errorString := fmt.Sprintf("Previously found boundary [%s]. This header also contains boundary [%s: %s].", boundary, header.Name, header.Value)
				panic(errorString)
			}

			log.Printf("Extracting boundary from header [%s: %s]\n", header.Name, header.Value)
			re := regexp.MustCompile(`boundary="([^\"]*)"`)
			matches := re.FindSubmatch([]byte(header.Value))
			if len(matches) > 2 {
				errorString := fmt.Sprintf("Found multiple matches for boundary [%q]", matches)
				panic(errorString)
			}
			if len(matches) <= 1 {
				boundary = readBoundaryTryAgain(header.Value)
			} else {
				boundary = string(matches[1])
			}

		}
	}

	if boundary == "" {
		log.Fatalf("Unable to find boundar in headers [%+v]", headers)
	}
	log.Printf("Found boundary [%s]\n", boundary)

	return boundary
}

func copyMessageExAttachments(m *gmail.Message) *gmail.Message {
	if m.Payload == nil {
		errorString := fmt.Sprintf("Message [%+v] must have a Payload", m)
		panic(errorString)
	}

	boundary := readBoundaryFromHeaders(m.Payload.Headers)

	rawPayload := convertPartToRawExAttachments(m.Payload, boundary, 0)

	rawPayload = base64.URLEncoding.EncodeToString([]byte(rawPayload))

	newMsg := gmail.Message{InternalDate: m.InternalDate, LabelIds: m.LabelIds, Payload: m.Payload, Raw: rawPayload, ThreadId: m.ThreadId}

	return &newMsg
}

func getMessagePartsRecursively(p *gmail.MessagePart, parts []*gmail.MessagePart) []*gmail.MessagePart {
	parts = append(parts, p)

	for _, subpart := range p.Parts {
		// recurse
		parts = getMessagePartsRecursively(subpart, parts)
	}

	return parts
}

func main() {
	fmt.Println("--------------------------------------------------------------------------------------------------------------------")
	ctx := context.Background()
	b, err := ioutil.ReadFile("credentials.json")
	if err != nil {
		log.Fatalf("Unable to read client secret file: %v", err)
	}

	// If modifying these scopes, delete your previously saved token.json.
	config, err := google.ConfigFromJSON(b, gmail.GmailReadonlyScope, gmail.GmailInsertScope, gmail.MailGoogleComScope)
	if err != nil {
		log.Fatalf("Unable to parse client secret file to config: %v", err)
	}
	client := getClient(config)

	service, err := gmail.NewService(ctx, option.WithHTTPClient(client))
	if err != nil {
		log.Fatalf("Unable to retrieve Gmail client: %v", err)
	}

	user := "me"

	argsWithProg := os.Args

	// Search for messages
	var queryString string
	defaultQueryString := "size:15000000"

	if len(argsWithProg) < 2 {
		queryString = defaultQueryString
		fmt.Printf("Using default query string [%v]\n", queryString)
	} else {
		queryString = argsWithProg[1]
		fmt.Printf("Using query string [%v]\n", queryString)
	}

	listMessagesReponse, err := service.Users.Messages.List(user).Q(queryString).Do()
	if err != nil {
		log.Fatalf("Unable to retrieve messages: %v", err)
	}
	if len(listMessagesReponse.Messages) == 0 {
		fmt.Println("No messages found.")
		return
	}
	fmt.Println("Messages:")
	fmt.Printf("Count: %+v\n", len(listMessagesReponse.Messages))

	// Get each message
	var messages []*gmail.Message

	for _, m := range listMessagesReponse.Messages {
		msg, _ := service.Users.Messages.Get(user, m.Id).Format("metadata").Do()
		messages = append(messages, msg)
	}

	// Sort by estimated size
	sort.Slice(messages, func(i, j int) bool {
		return messages[i].SizeEstimate < messages[j].SizeEstimate
	})

	// Get each message, make a copy without attachments, and insert the copy
	for _, msg := range messages {
		fmt.Println("------------------------------")
		fmt.Println("Message:")
		fmt.Printf("Id: %+v\n", msg.Id)
		fmt.Printf("Snippet: %+v\n", msg.Snippet)
		fmt.Printf("SizeEstimate: %+v\n", msg.SizeEstimate)
		fmt.Printf("LabelIds: %+v\n", msg.LabelIds)
		fmt.Println("Headers:")
		for _, header := range msg.Payload.Headers {
			fmt.Printf("* %+v: %+v\n", header.Name, header.Value)
		}
		fmt.Println("Body:")
		if msg.Payload != nil && msg.Payload.Body != nil {
			fmt.Printf("%+v", msg.Payload.Body.Data)
		}

		rawMsg, _ := service.Users.Messages.Get(user, msg.Id).Format("raw").Do()
		fmt.Println("-------------RAW DECODED MESSAGE--------------------")
		decodedMsg, _ := base64.URLEncoding.DecodeString(rawMsg.Raw)
		fmt.Printf("%+v\n", string(decodedMsg))
		fmt.Println("----------------------------------------------------")

		fullMsg, _ := service.Users.Messages.Get(user, msg.Id).Format("full").Do()
		boundary := readBoundaryFromHeaders(fullMsg.Payload.Headers)
		fullMsgPayloadExAttachments := convertPartToRawExAttachments(fullMsg.Payload, boundary, 0)
		fmt.Println("-------------RAW MESSAGE EX ATTACHMENTS--------------------")
		fmt.Printf("%+v\n", fullMsgPayloadExAttachments)
		fmt.Println("----------------------------------------------------")

		// TODO: Comparing the message without attachments to the original message will of course be different.
		//       Need to add unit tests instead. Download msg, encode base64 raw, compare to raw message,
		//       insert new message, download new message, compare parts to original message. delete/clean up.
		// if fullMsgPayloadExAttachments != string(decodedMsg) {
		// 	fmt.Printf("CAUTION. STRINGS ARE NOT IDENTICAL. DIFF:")
		// 	fmt.Printf("%+v\n", diff.Diff(string(decodedMsg), fullMsgPayloadExAttachments))
		// }

		var parts []*gmail.MessagePart
		parts = getMessagePartsRecursively(fullMsg.Payload, parts)

		// Useful reference: https://stackoverflow.com/questions/25832631/download-attachments-from-gmail-using-gmail-api
		var attachments []string
		for _, part := range parts {
			if part.Filename != "" && part.Body.AttachmentId != "" {
				attachmentId := part.Body.AttachmentId

				log.Printf("Getting attachment with ID [%+v].\n", attachmentId)
				attachment, err := service.Users.Messages.Attachments.Get(user, msg.Id, attachmentId).Do()
				if err != nil {
					log.Fatalf("Unable to get attachment [%+v].\n", err)
				}

				attachments = append(attachments, fmt.Sprintf("* %+v: %+v", part.Filename, attachment.Size))
			}
		}

		fmt.Printf("Attachments (%+v):\n", len(attachments))
		for _, a := range attachments {
			fmt.Println(a)
		}

		if len(attachments) == 0 {
			log.Printf("No attachments found on message [%+v].\n", msg.Id)
			continue
		}

		fmt.Println("Do you want to delete the attachments from this email? (y or n)")
		var yesOrNo string
		fmt.Scanln(&yesOrNo)
		yesOrNo = strings.ToLower(yesOrNo)

		if yesOrNo != "y" && yesOrNo != "yes" && yesOrNo != "n" && yesOrNo != "no" {
			log.Fatalf("Invalid input. Allowed values are [y, yes, n, no]. Exiting.")
		}

		if yesOrNo == "n" || yesOrNo == "no" {
			log.Printf("Skipped message [%+v]\n", msg.Id)
			continue
		}

		log.Println("Copying message [%+v]\n", fullMsg.Id)
		// Use original date of message: InternalDateSource('dateHeader'). See also:
		// * https://developers.google.com/gmail/api/reference/rest/v1/InternalDateSource
		// * https://stackoverflow.com/questions/46434390/remove-an-attachment-of-a-gmail-email-with-google-apps-script
		newMsg := copyMessageExAttachments(fullMsg)

		log.Println("Inserting copied message without attachments.")
		insertResponse, err := service.Users.Messages.Insert(user, newMsg).InternalDateSource("dateHeader").Do()
		if err != nil {
			log.Fatalf("Unable to insert message: %v\n", err)
		}

		log.Println("Insert Response[%+v]\n", insertResponse)

		log.Printf("Deleting original message [%+v]\n", msg)
		err = service.Users.Messages.Delete(user, msg.Id).Do()
		if err != nil {
			log.Fatalf("Unable to delete message: %v\n", err)
		}

	}

	fmt.Println("|||||||||||||||||||||||||||||||||||||||||||||||||||||||")
	fmt.Println("Querying again...")

	listMessagesReponse, err = service.Users.Messages.List(user).Q(queryString).Do()
	if err != nil {
		log.Fatalf("Unable to retrieve messages: %v", err)
	}
	if len(listMessagesReponse.Messages) == 0 {
		fmt.Println("No messages found.")
		return
	}
	fmt.Println("Messages:")
	fmt.Printf("Count: %+v\n", len(listMessagesReponse.Messages))
}

// [END gmail_quickstart]

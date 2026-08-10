package eino

import (
	"testing"

	"github.com/cloudwego/eino/schema"
)

func TestToADKMessagesAddsNativeVisualInput(t *testing.T) {
	messages := toADKMessages("inspect this page", nil, []VisualInput{{
		ID: "image-1", MIMEType: "image/png", Data: []byte("image-data"), SHA256: "abc", Detail: "high",
	}})
	if len(messages) != 1 {
		t.Fatalf("messages = %#v", messages)
	}
	message := messages[0]
	if message == nil || len(message.UserInputMultiContent) != 2 {
		t.Fatalf("multimodal message = %#v", messages[0])
	}
	image := message.UserInputMultiContent[1].Image
	if image == nil || image.Base64Data == nil || image.MIMEType != "image/png" || image.Detail != schema.ImageURLDetailHigh {
		t.Fatalf("image part = %#v", image)
	}
}

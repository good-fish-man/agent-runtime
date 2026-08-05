package eino

import (
	"context"
	"strings"
	"testing"

	"github.com/good-fish-man/agent-runtime/internal/tools"

	"github.com/cloudwego/eino/components/tool"
	"github.com/cloudwego/eino/schema"
)

type fakeImageTool struct {
	input string
}

func (f *fakeImageTool) Info(context.Context) (*schema.ToolInfo, error) {
	return &schema.ToolInfo{Name: tools.GenerateImageToolName}, nil
}

func (f *fakeImageTool) InvokableRun(_ context.Context, input string, _ ...tool.Option) (string, error) {
	f.input = input
	return "![Generated image](https://example.com/image.png)", nil
}

func TestExecuteTextToolMarkupInvokesRegisteredImageTool(t *testing.T) {
	imageTool := &fakeImageTool{}
	content := `<tools> {"name":"GenerateImage","arguments":{"prompt":"a lighthouse","size":"1024x1024"}} </tools>`
	result, handled, err := executeTextToolMarkup(context.Background(), content, []tool.BaseTool{imageTool})
	if err != nil {
		t.Fatal(err)
	}
	if !handled || result != "![Generated image](https://example.com/image.png)" {
		t.Fatalf("result = %q, handled = %v", result, handled)
	}
	if !strings.Contains(imageTool.input, `"prompt":"a lighthouse"`) {
		t.Fatalf("tool input = %s", imageTool.input)
	}
}

func TestExecuteTextToolMarkupExplainsMissingImageModel(t *testing.T) {
	result, handled, err := executeTextToolMarkup(context.Background(), `<tools>{"name":"GenerateImage","arguments":{"prompt":"cat"}}</tools>`, nil)
	if err != nil {
		t.Fatal(err)
	}
	if !handled || !strings.Contains(result, "未绑定图片生成模型") {
		t.Fatalf("result = %q, handled = %v", result, handled)
	}
}

func TestExecuteTextToolMarkupRejectsOtherTools(t *testing.T) {
	result, handled, err := executeTextToolMarkup(context.Background(), `<tools>{"name":"Bash","arguments":{"command":"whoami"}}</tools>`, nil)
	if err != nil {
		t.Fatal(err)
	}
	if !handled || !strings.Contains(result, "不受支持") {
		t.Fatalf("result = %q, handled = %v", result, handled)
	}
}

func TestExecuteTextToolMarkupRejectsClientActions(t *testing.T) {
	result, handled, err := executeTextToolMarkup(context.Background(), `<tools>{"name":"browser_open","arguments":{"target":"Acme Portal"}}</tools>`, nil)
	if err != nil {
		t.Fatal(err)
	}
	if !handled || !strings.Contains(result, "不受支持") {
		t.Fatalf("result=%q handled=%v", result, handled)
	}
}

func TestExecuteTextToolMarkupHandlesMalformedMarkup(t *testing.T) {
	result, handled, err := executeTextToolMarkup(context.Background(), `<tools>{"name":"GenerateImage"}`, nil)
	if err != nil {
		t.Fatal(err)
	}
	if !handled || !strings.Contains(result, "格式无效") {
		t.Fatalf("result = %q, handled = %v", result, handled)
	}
}

func TestExecuteTextToolMarkupDoesNotTreatNormalTextAsToolCall(t *testing.T) {
	content := `You can write <tools> in documentation.`
	result, handled, err := executeTextToolMarkup(context.Background(), content, nil)
	if err != nil {
		t.Fatal(err)
	}
	if handled || result != content {
		t.Fatalf("result = %q, handled = %v", result, handled)
	}
}

func TestToolMarkupStreamFilterPassesNormalTextImmediately(t *testing.T) {
	var output strings.Builder
	filter := newToolMarkupStreamFilter(func(chunk StreamChunk) error {
		output.WriteString(chunk.Text)
		return nil
	})
	if err := filter.write(StreamChunk{Text: "hello"}); err != nil {
		t.Fatal(err)
	}
	if output.String() != "hello" {
		t.Fatalf("output = %q", output.String())
	}
}

func TestToolMarkupStreamFilterBuffersAndExecutesMarkup(t *testing.T) {
	var output strings.Builder
	imageTool := &fakeImageTool{}
	filter := newToolMarkupStreamFilter(func(chunk StreamChunk) error {
		output.WriteString(chunk.Text)
		return nil
	})
	for _, text := range []string{"<to", "ols>", `{"name":"GenerateImage","arguments":{"prompt":"cat"}}`, "</tools>"} {
		if err := filter.write(StreamChunk{Text: text}); err != nil {
			t.Fatal(err)
		}
	}
	if output.Len() != 0 {
		t.Fatalf("markup leaked before completion: %q", output.String())
	}
	result, handled, err := filter.finish(context.Background(), []tool.BaseTool{imageTool})
	if err != nil {
		t.Fatal(err)
	}
	if !handled || result != output.String() || !strings.HasPrefix(result, "![Generated image]") {
		t.Fatalf("result = %q, output = %q, handled = %v", result, output.String(), handled)
	}
}

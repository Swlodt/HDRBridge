package apperr

import (
	"errors"
	"fmt"
	"strings"
	"testing"
)

func TestPublicMessageSurvivesInternalWrapping(t *testing.T) {
	cause := errors.New("internal database /private/storage.db")
	err := fmt.Errorf("保存图片 image-123: %w", Wrap(ErrConflict, "图片暂时不可修改", cause))
	if Message(err) != "操作冲突: 图片暂时不可修改" {
		t.Fatalf("公开提示包含内部信息: %s", Message(err))
	}
	if !errors.Is(err, ErrConflict) || !errors.Is(err, cause) {
		t.Fatal("包装丢失分类或底层错误链")
	}
	if !strings.Contains(err.Error(), cause.Error()) || !strings.Contains(err.Error(), "image-123") {
		t.Fatal("诊断错误丢失上下文")
	}
	if Message(fmt.Errorf("internal /private/path: %w", ErrInvalid)) != "参数无效" {
		t.Fatal("普通包装泄露内部信息")
	}
	if Message(cause) != "服务器内部错误" {
		t.Fatal("未分类错误泄露内部信息")
	}
}

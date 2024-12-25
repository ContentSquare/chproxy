package cache

import (
	"io"
	"log"
	"net/http"
	"os"
	"testing"
)

type FakeResponse struct {
	t       *testing.T
	headers http.Header
	body    []byte
	status  int
}

func newFakeResponse() *FakeResponse {
	resp := &FakeResponse{
		headers: make(http.Header),
	}
	resp.headers.Set("Content-Type", "content-type-1")
	resp.headers.Set("Content-Encoding", "content-encoding-1")

	return resp
}

func (r *FakeResponse) Header() http.Header {
	return r.headers
}

func (r *FakeResponse) Write(body []byte) (int, error) {
	r.body = body
	return len(body), nil
}

func (r *FakeResponse) WriteHeader(status int) {
	r.status = status
}

func (r *FakeResponse) CloseNotify() <-chan bool {
	return nil
}

const testTmpWriterDir = "./test-tmp-data"

func init() {
	if err := os.RemoveAll(testTmpWriterDir); err != nil {
		log.Fatalf("cannot remove %q: %s", testTmpWriterDir, err)

	}
	err := os.Mkdir(testTmpWriterDir, 0777)
	if err != nil && !os.IsExist(err) {
		log.Fatal(err)
	}
}

func TestFileCreation(t *testing.T) {
	srw := newFakeResponse()
	files, _ := os.ReadDir(testTmpWriterDir)
	nbFileBefore := len(files)

	tmpFileRespWriter, err := NewTmpFileResponseWriter(srw, testTmpWriterDir)
	defer tmpFileRespWriter.Close()
	if err != nil {
		t.Fatalf("could not initate TmpFileResponseWriter error:%s", err)
		return
	}

	files, _ = os.ReadDir(testTmpWriterDir)
	nbFileAfter := len(files)
	if nbFileAfter == nbFileBefore {
		t.Fatalf("Error while creating tmp file")
		return
	}
}

func TestFileRemoval(t *testing.T) {
	srw := newFakeResponse()
	files, _ := os.ReadDir(testTmpWriterDir)
	nbFileBefore := len(files)

	tmpFileRespWriter, err := NewTmpFileResponseWriter(srw, testTmpWriterDir)
	if err != nil {
		t.Fatalf("could not initate TmpFileResponseWriter error:%s", err)
		return
	}
	tmpFileRespWriter.Close()

	files, _ = os.ReadDir(testTmpWriterDir)
	nbFileAfter := len(files)
	if nbFileAfter != nbFileBefore {
		t.Fatalf("Error while deleting tmp file")
		return
	}
}

func TestWriteThenReadHeader(t *testing.T) {
	srw := newFakeResponse()

	tmpFileRespWriter, err := NewTmpFileResponseWriter(srw, testTmpWriterDir)
	defer tmpFileRespWriter.Close()
	if err != nil {
		t.Fatalf("could not initate TmpFileResponseWriter error:%s", err)
		return
	}
	tmpFileRespWriter.Write([]byte("this is a test1 of length 28"))

	cType := tmpFileRespWriter.GetCapturedContentType()
	cEncoding := tmpFileRespWriter.GetCapturedContentEncoding()
	cLength, _ := tmpFileRespWriter.GetCapturedContentLength()
	if err != nil {
		t.Fatalf("could not get ContentLength error:%s", err)
		return
	}

	if cType != "content-type-1" {
		t.Fatalf("wrong value for contentType, got %s, expected %s", cType, "content-type-1")
	}
	if cEncoding != "content-encoding-1" {
		t.Fatalf("wrong value for contentEncoding, got %s, expected %s", cEncoding, "content-encoding-1")
	}
	if cLength != 28 {
		t.Fatalf("wrong value for contentLength, got %d, expected %d", cLength, 28)
	}

}

func TestWriteThenReadContent(t *testing.T) {
	srw := newFakeResponse()

	tmpFileRespWriter, err := NewTmpFileResponseWriter(srw, testTmpWriterDir)
	defer tmpFileRespWriter.Close()
	if err != nil {
		t.Fatalf("could not initate TmpFileResponseWriter error:%s", err)
		return
	}
	expectedContent := "test content"
	_, err = tmpFileRespWriter.Write([]byte(expectedContent))
	if err != nil {
		t.Fatalf("could not write into tmp file:%s", err)
		return
	}
	reader, err := tmpFileRespWriter.Reader()
	if err != nil {
		t.Fatalf("could not read tmp file:%s", err)
		return
	}
	tmpFileRespWriter.ResetFileOffset()
	buffer, err := io.ReadAll(reader)
	if err != nil {
		t.Fatalf("could not read tmp file:%s", err)
		return
	}
	if len(buffer) == 0 {
		t.Fatalf("read 0 bytes file:")
		return
	}
	s := string(buffer)

	if s != expectedContent {
		t.Fatalf("wrong value for contentLength, got %s, expected %s", s, expectedContent)
	}
}

func TestWriteThenReadStatusCode(t *testing.T) {
	srw := newFakeResponse()
	expectStatusCode1 := http.StatusOK
	expectStatusCode2 := 444
	tmpFileRespWriter, err := NewTmpFileResponseWriter(srw, testTmpWriterDir)
	defer tmpFileRespWriter.Close()
	if err != nil {
		t.Fatalf("could not initate TmpFileResponseWriter error:%s", err)
		return
	}
	statusCode := tmpFileRespWriter.StatusCode()

	if expectStatusCode1 != statusCode {
		t.Fatalf("wrong value for statusCode, got %d, expected %d", statusCode, expectStatusCode1)

	}

	tmpFileRespWriter.WriteHeader(expectStatusCode2)
	statusCode = tmpFileRespWriter.StatusCode()
	if expectStatusCode2 != statusCode {
		t.Fatalf("wrong value for statusCode, got %d, expected %d", statusCode, expectStatusCode2)

	}

}

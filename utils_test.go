package main

import (
	"bytes"
	"compress/gzip"
	"fmt"
	"github.com/stretchr/testify/assert"
	"net/http"
	"net/url"
	"testing"
)

func TestSkipLeadingComments(t *testing.T) {
	testSkipLeadingComments(t, "", "")
	testSkipLeadingComments(t, "a", "a")
	testSkipLeadingComments(t, "SELECT 1", "SELECT 1")
	testSkipLeadingComments(t, "\t\n\v\f\r aaa  ", "aaa  ")
	testSkipLeadingComments(t, "\t  /** foo /* */ bar ", "bar ")
	testSkipLeadingComments(t, "/* foo *//* bar */\t\t/* baz */aaa", "aaa")
	testSkipLeadingComments(t, "   /*  sdfsd * dfds / sdf", "")
	testSkipLeadingComments(t, "  -- sdsfd - -- -", "")
	testSkipLeadingComments(t, "\t - sss", "- sss")
	testSkipLeadingComments(t, " -- ss\n xdf", "xdf")
	testSkipLeadingComments(t, " --\n /**/-- /* ssd \n/* xdfd */   qqw ", "qqw ")
}

func testSkipLeadingComments(t *testing.T, q, expectedQ string) {
	t.Helper()
	s := skipLeadingComments([]byte(q))
	if string(s) != expectedQ {
		t.Fatalf("unexpected result %q; expecting %q", s, expectedQ)
	}
}

func TestSortHeaders(t *testing.T) {
	testSortHeaders(t, "br, gzip, deflate", "br,deflate,gzip")
	testSortHeaders(t, "br,     gzip, deflate", "br,deflate,gzip")
	testSortHeaders(t, "gzip,br,deflate", "br,deflate,gzip")
	testSortHeaders(t, "gzip", "gzip")
	testSortHeaders(t, "deflate, gzip, br", "br,deflate,gzip")
}

func testSortHeaders(t *testing.T, h, expectedH string) {
	t.Helper()
	s := sortHeader(h)
	if s != expectedH {
		t.Fatalf("unexpected result %q; expecting %q", s, expectedH)
	}
}

func TestCanCacheQuery(t *testing.T) {
	testCanCacheQuery(t, "", false)
	testCanCacheQuery(t, "   ", false)
	testCanCacheQuery(t, "INSERT aaa", false)
	testCanCacheQuery(t, "\t  INSERT aaa   ", false)
	testCanCacheQuery(t, "select", true)
	testCanCacheQuery(t, "\t\t   SELECT 123   ", true)
	testCanCacheQuery(t, "\t\t   sElECt 123   ", true)
	testCanCacheQuery(t, "   --- sd s\n /* dfsf */\n seleCT ", true)
	testCanCacheQuery(t, "   --- sd s\n /* dfsf */\n insert ", false)
	testCanCacheQuery(t, "WITH 1 as alias SELECT alias FROM nothing ", true)
}

func testCanCacheQuery(t *testing.T, q string, expected bool) {
	t.Helper()
	canCache := canCacheQuery([]byte(q))
	if canCache != expected {
		t.Fatalf("unexpected result %v; expecting %v", canCache, expected)
	}
}

func TestGetQuerySnippetGET(t *testing.T) {
	req, err := http.NewRequest("GET", "", nil)
	checkErr(t, err)
	params := make(url.Values)
	q := "SELECT column FROM table"
	params.Set("query", q)
	req.URL.RawQuery = params.Encode()
	query := getQuerySnippet(req)
	if query != q {
		t.Fatalf("got: %q; expected: %q", query, q)
	}
}

func TestGetQuerySnippetGETBody(t *testing.T) {
	q := "SELECT column FROM table"
	body := bytes.NewBufferString(q)
	req, err := http.NewRequest("GET", "", body)
	checkErr(t, err)
	query := getQuerySnippet(req)
	if query != q {
		t.Fatalf("got: %q; expected: %q", query, q)
	}
}

func TestGetQuerySnippetGETBothQueryAndBody(t *testing.T) {
	queryPart := "SELECT column"
	bodyPart := "FROM table"
	expectedQuery := "SELECT column\nFROM table"

	body := bytes.NewBufferString(bodyPart)
	req, err := http.NewRequest("GET", "", body)
	checkErr(t, err)

	params := make(url.Values)
	params.Set("query", queryPart)
	req.URL.RawQuery = params.Encode()

	query := getQuerySnippet(req)
	if query != expectedQuery {
		t.Fatalf("got: %q; expected: %q", query, expectedQuery)
	}
}

func TestGetQuerySnippetPOST(t *testing.T) {
	q := "SELECT column FROM table"
	body := bytes.NewBufferString(q)
	req, err := http.NewRequest("POST", "", body)
	checkErr(t, err)
	query := getQuerySnippet(req)
	if query != q {
		t.Fatalf("got: %q; expected: %q", query, q)
	}
}

func TestGetQuerySnippetGzipped(t *testing.T) {
	var buf bytes.Buffer
	zw := gzip.NewWriter(&buf)
	q := makeQuery(1000)
	_, err := zw.Write([]byte(q))
	if err != nil {
		t.Fatal(err)
	}
	zw.Close()
	req, err := http.NewRequest("POST", "http://127.0.0.1:9090", &buf)
	req.Header.Set("Content-Encoding", "gzip")
	if err != nil {
		t.Fatal(err)
	}
	query := getQuerySnippet(req)
	if query[:100] != string(q[:100]) {
		t.Fatalf("got: %q; expected: %q", query[:100], q[:100])
	}
}

func TestGetFullQueryGzipped(t *testing.T) {
	var buf bytes.Buffer
	zw := gzip.NewWriter(&buf)
	q := makeQuery(1000)
	_, err := zw.Write([]byte(q))
	if err != nil {
		t.Fatal(err)
	}
	zw.Close()
	req, err := http.NewRequest("POST", "http://127.0.0.1:9090", &buf)
	req.Header.Set("Content-Encoding", "gzip")
	if err != nil {
		t.Fatal(err)
	}
	query, err := getFullQuery(req)
	if err != nil {
		t.Fatal(err)
	}
	checkResponse(t, req.Body, buf.String())
	if string(query) != string(q) {
		t.Fatalf("got: %q; expected %q", query, q)
	}
}

var (
	testQuery     = "SELECT column col0, col1, col2, col3, col4, col5, col6, col7, col8, col9, col10, col11, col12, col13, col14, col15, col16, col17, col18, col19, col20, col21, col22, col23, col24, col25, col26, col27, col28, col29, col30, col31, col32, col33, col34, col35, col36, col37, col38, col39, col40, col41, col42, col43, col44, col45, col46, col47, col48, col49, col50, col51, col52, col53, col54, col55, col56, col57, col58, col59, col60, col61, col62, col63, col64, col65, col66, col67, col68, col69, col70, col71, col72, col73, col74, col75, col76, col77, col78, col79, col80, col81, col82, col83, col84, col85, col86, col87, col88, col89, col90, col91, col92, col93, col94, col95, col96, col97, col98, col99, col100, col101, col102, col103, col104, col105, col106, col107, col108, col109, col110, col111, col112, col113, col114, col115, col116, col117, col118, col119, col120, col121, col122, col123, col124, col125, col126, col127, col128, col129, col130, col131, col132, col133, col134, col135, col136, col137, col138, col139, col140, col141, col142, col143, col144, col145, col146, col147, col148, col149, col150, col151, col152, col153, col154, col155, col156, col157, col158, col159, col160, col161, col162, col163, col164, col165, col166, col167, col168, col169, col170, col171, col172, col173, col174, col175, col176, col177, col178, col179, col180, col181, col182, col183, col184, col185, col186, col187, col188, col189, col190, col191, col192, col193, col194, col195, col196, col197, col198, col199, WHERE Date=today()\n"
	lz4TestQuery  = "\xfb\xd7NϹ\xec\xf2\x81Hp`\xe3'A(>\x82N\x03\x00\x00\xf3\x05\x00\x00\xd0SELECT column\a\x00 0,\x06\x00\x111\x06\x00\x112\x06\x00\x113\x06\x00\x114\x06\x00\x115\x06\x00\x116\x06\x00\x117\x06\x00\x118\x06\x00\x119\x06\x00\x131=\x00\x02>\x00\x121?\x00\x121@\x00\x121A\x00\x121B\x00\x121C\x00\x121D\x00\x121E\x00\x121F\x00\x122F\x00\x122F\x00\x122F\x00\x122F\x00\x122F\x00\x122F\x00\x122F\x00\x122F\x00\x122F\x00\x122F\x00\x123F\x00\x123F\x00\x123F\x00\x123F\x00\x123F\x00\x123F\x00\x123F\x00\x123F\x00\x123F\x00\x123F\x00\x124F\x00\x124F\x00\x124F\x00\x124F\x00\x124F\x00\x124F\x00\x124F\x00\x124F\x00\x124F\x00\x124F\x00\x125F\x00\x125F\x00\x125F\x00\x125F\x00\x125F\x00\x125F\x00\x125F\x00\x125F\x00\x125F\x00\x125F\x00\x126F\x00\x126F\x00\x126F\x00\x126F\x00\x126F\x00\x126F\x00\x126F\x00\x126F\x00\x126F\x00\x126F\x00\x127F\x00\x127F\x00\x127F\x00\x127F\x00\x127F\x00\x127F\x00\x127F\x00\x127F\x00\x127F\x00\x127F\x00\x128F\x00\x128F\x00\x128F\x00\x128F\x00\x128F\x00\x128F\x00\x128F\x00\x128F\x00\x128F\x00\x128F\x00\x129F\x00\x129F\x00\x129F\x00\x129F\x00\x129F\x00\x129F\x00\x129F\x00\x129F\x00\x129F\x00\x129F\x00\"10G\x00\"10H\x00\"10I\x00\"10J\x00\"10K\x00\"10L\x00\"10M\x00\"10N\x00\"10O\x00#10P\x00\x05\xc7\x02\x03P\x00\x04\xc9\x02\x04\xca\x02\x04\xcb\x02\x04\xcc\x02\x04\xcd\x02\x04\xce\x02\x04\xcf\x02\x03\xd0\x02\x131\xd1\x02\x131\xd2\x02\x131\xd3\x02\x131\xd4\x02\x131\xd5\x02\x131\xd6\x02\x131\xd7\x02\x131\xd8\x02\x131\xd9\x02#12\xa0\x00\x03\xdb\x02\x131\xdc\x02\x131\xdd\x02\x131\xde\x02\x131\xdf\x02\x131\xe0\x02\x131\xe1\x02\x131\xe2\x02\x131\xe3\x02\x131\xe4\x02\x131\xe5\x02\x131\xe6\x02\x131\xe7\x02\x131\xe8\x02\x131\xe9\x02\x131\xea\x02\x131\xeb\x02\x131\xec\x02\x131\xed\x02\x131\xee\x02\x131\xef\x02\x131\xf0\x02\x131\xf1\x02\x131\xf2\x02\x131\xf3\x02\x131\xf4\x02\x131\xf5\x02\x131\xf6\x02\x131\xf7\x02\x131\xf8\x02\x131\xf9\x02\x131\xfa\x02\x131\xfb\x02\x131\xfc\x02\x131\xfd\x02\x131\xfe\x02\x131\xff\x02\x131\x00\x03\x131\x01\x03\x131\x02\x03\x131\x03\x03\x131\x04\x03\x131\x05\x03\x131\x06\x03\x131\a\x03\x131\b\x03#170\x02\x03\n\x03\x131\v\x03\x131\f\x03\x131\r\x03\x131\x0e\x03\x131\x0f\x03\x131\x10\x03\x131\x11\x03\x131\x12\x03\x131\x13\x03\x131\x14\x03\x131\x15\x03\x131\x16\x03\x131\x17\x03\x131\x18\x03\x131\x19\x03\x131\x1a\x03\x131\x1b\x03\x131\x1c\x03\x131\x1d\x03\x131\x1e\x03\x131\x1f\x03\x101 \x03\xf0\x04WHERE Date=today()\n"
	zstdTestQuery = "\x14\x85\x91\xdd\xe6^1o$\xd6u\xb8m}=ǐ)\x01\x00\x00\xf3\x05\x00\x00(\xb5/\xfd`\xf3\x04\xb5\b\x00\xc6\x110\x1bp\v\x01\x03p\x1bD\"&^\r\xd7\xfc>\xdc\x0e\xa0\x93\xdddgD\xde\nU\x05\x06,\x00$\x00#\x00\xa5\xc9\f.Mfli2CK\x93\x19Y\x9a\xcc\xc0\xd2dƕ&3\xac4\x99Q\xa5\xc9\f*M\xe6\xc1\x01\xa24\x19\x05\x82\xc4A\"!̻\xbb\xbb\xbb\xbb\xaa\xaa\xaa\xaa\xaa\x99\x99\x99\x99\x99\x88\x88\x88\x88\x88wwwwwfffffUUUU\x0f\x89\x88\x88\x88\x88xwwwwgffffVUUUUUDDDDD\xf5\xff\xff\xff3333\a\x00\x02\x01\x11\n\x86^\f4,\x14\x05\x93@H\x18,\xcc\xf8\xff\xff\xff\xcf\xcc\xcc\xcc̼\xbb\xbb\xbb\xbb\xab\xaa\xaa\xaa\xaa\x9a\x99\x99\x99\x99\x01\x80\xbe\xa8\x01\xf8\xf4\xff\x03\xc03\xac\x06\x10\x84\x1f\xef\xff\xf7\xf9\xff}\xf7\u007f\u07ff\xdf\xf7\xff\xf6\xfeLԠ$(\aʀ\xf2\x9f\xec'\xf7\xc9|\xf2\x9e\xac\xe9\t\x00@H\x00\x10\x02\x00\x84\x00\x00!\x00\x94\x84 \xc4A\fR\x10\x82\fD P@~\x80\xfc\x91,~\x1f"
)

func TestDecompression(t *testing.T) {
	testCases := []struct {
		name            string
		compressedQuery string
		f               func(*http.Request) error
	}{
		{
			"full LZ4",
			lz4TestQuery,
			func(req *http.Request) error {
				q, err := getFullQuery(req)
				if err != nil {
					return err
				}
				checkResponse(t, req.Body, lz4TestQuery)
				if string(q) != testQuery {
					return fmt.Errorf("got: %q; expected %q", string(q), testQuery)
				}
				return nil
			},
		},
		{
			"snippet LZ4",
			lz4TestQuery,
			func(req *http.Request) error {
				q := getQuerySnippet(req)
				if q[:100] != string(testQuery[:100]) {
					return fmt.Errorf("got: %q; expected: %q", q[:100], testQuery[:100])
				}
				return nil
			},
		},
		{
			"partial LZ4",
			lz4TestQuery + "foobar", // write whatever to buf to make the data partially invalid
			func(req *http.Request) error {
				q := getQuerySnippet(req)
				if q[:50] != testQuery[:50] {
					return fmt.Errorf("got: %q; expected: %q", q[:50], testQuery[:50])
				}
				return nil
			},
		},
		{
			"invalid compression",
			"foobar", // write totally invalid data and treat it as compressed
			func(req *http.Request) error {
				q := getQuerySnippet(req)
				if q != "foobar" {
					t.Fatalf("got: %q; expected: %q", q, "foobar")
				}
				return nil
			},
		},
		{
			"full ZSTD",
			zstdTestQuery,
			func(req *http.Request) error {
				q, err := getFullQuery(req)
				if err != nil {
					return err
				}
				checkResponse(t, req.Body, zstdTestQuery)
				if string(q) != testQuery {
					return fmt.Errorf("got: %q; expected %q", string(q), testQuery)
				}
				return nil
			},
		},
		{
			"snippet ZSTD",
			zstdTestQuery,
			func(req *http.Request) error {
				q := getQuerySnippet(req)
				if q[:100] != string(testQuery[:100]) {
					return fmt.Errorf("got: %q; expected: %q", q[:100], testQuery[:100])
				}
				return nil
			},
		},
		{
			"partial ZSTD",
			zstdTestQuery + "foobar", // write whatever to buf to make the data partially invalid
			func(req *http.Request) error {
				q := getQuerySnippet(req)
				if q[:50] != testQuery[:50] {
					return fmt.Errorf("got: %q; expected: %q", q[:50], testQuery[:50])
				}
				return nil
			},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			r := bytes.NewBufferString(tc.compressedQuery)
			req, err := http.NewRequest("POST", "http://127.0.0.1:9090?decompress=1", r)
			if err != nil {
				t.Fatal(err)
			}
			if err := tc.f(req); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestGetSessionTimeout(t *testing.T) {
	req, err := http.NewRequest(http.MethodPost, "http://127.0.0.1:9090", nil)
	if err != nil {
		panic(err)
	}
	params := make(url.Values)
	// uint str return self
	firstSessionTimeout := "888"
	// invalid str return 60
	secondSessionTimeout := "600s"
	thirdSessionTimeout := "aaa"
	params.Add("query", "SELECT 1")
	params.Set("session_timeout", firstSessionTimeout)
	req.URL.RawQuery = params.Encode()
	if getSessionTimeout(req) != 888 {
		t.Fatalf("user set session_timeout %q; get %q , expected %q ", firstSessionTimeout, getSessionTimeout(req), 888)
	}
	params.Set("session_timeout", secondSessionTimeout)
	req.URL.RawQuery = params.Encode()
	if getSessionTimeout(req) != 60 {
		t.Fatalf("user set session_timeout %q; get %q , expected %q", secondSessionTimeout, getSessionTimeout(req), 60)
	}
	params.Set("session_timeout", thirdSessionTimeout)
	req.URL.RawQuery = params.Encode()
	if getSessionTimeout(req) != 60 {
		t.Fatalf("user set session_timeout %q; get %q , expected %q", thirdSessionTimeout, getSessionTimeout(req), 60)
	}
	params.Del("session_timeout")
	req.URL.RawQuery = params.Encode()
	if getSessionTimeout(req) != 60 {
		t.Fatalf("user not set session_timeout ,get %q , expected %q", getSessionTimeout(req), 60)
	}
}

func makeQuery(n int) []byte {
	q1 := "SELECT column "
	q2 := "WHERE Date=today()"

	var b []byte
	b = append(b, q1...)
	for i := 0; i < n; i++ {
		b = append(b, fmt.Sprintf("col%d, ", i)...)
	}
	b = append(b, q2...)
	return b
}

func TestCalcMapHash(t *testing.T) {
	testCases := []struct {
		name           string
		input          map[string]string
		expectedResult uint32
		expectedError  error
	}{
		{
			"nil map",
			nil,
			0,
			nil,
		},
		{
			"empty map",
			map[string]string{},
			0,
			nil,
		},
		{
			"map with value",
			map[string]string{"param_table_name": "clients"},
			0x40802c7a, // write whatever to buf to make the data partially invalid
			nil,
		},
		{
			"map with multiple value",
			map[string]string{"param_table_name": "clients", "param_database": "default"},
			0x6fddf04d,
			nil,
		},
		{
			"map with exchange value",
			map[string]string{"param_database": "default", "param_table_name": "clients"},
			0x6fddf04d,
			nil,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			r, err := calcMapHash(tc.input)
			assert.Equal(t, r, tc.expectedResult)
			assert.Equal(t, err, tc.expectedError)
		})
	}
}

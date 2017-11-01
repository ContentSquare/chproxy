package main

import (
	"bytes"
	"io/ioutil"
	"net/http/httptest"
	"testing"
)

func TestCachedReadCloser(t *testing.T) {
	b := makeQuery(1000)
	crc := &cachedReadCloser{
		ReadCloser: ioutil.NopCloser(bytes.NewReader(b)),
	}
	req := httptest.NewRequest("POST", "http://localhost", crc)
	res, err := getFullQuery(req)
	if err != nil {
		t.Fatalf("cannot obtain response: %s", err)
	}
	if string(res) != string(b) {
		t.Fatalf("unexpected query read %q; expecting %q", res, b)
	}

	expectedStart := "SELECT column col0, col1, col2, col3, col4, col5, col6, col7, col8, col9, col10, col11, col12, col13, col14, col15, col16, col17, col18, col19, col20, col21, col22, col23, col24, col25, col26, col27, col28, col29, col30, col31, col32, col33, col34, col35, col36, col37, col38, col39, col40, col41, col42, col43, col44, col45, col46, col47, col48, col49, col50, col51, col52, col53, col54, col55, col56, col57, col58, col59, col60, col61, col62, col63, col64, col65, col66, col67, col68, col69, col70, col71, col72, col73, col74, col75, col76, col77, col78, col79, col80, col81, col82, col83, col84, col85, col86, col87, col88, col89, col90, col91, col92, col93, col94, col95, col96, col97, col98, col99, col100, col101, col102, col103, col104, col105, col106, col107, col108, col109, col110, col111, col112, col113, col114, col115, col116, col117, col118, col119, col120, col121, col122, col123, col124, col125, col126, col127, col128, col129, col130, col131, col132, col133, col134, col135, col136, col137, col138, col139, col140, col141, col142, col143, col144, col145, col146, col147, col148, col149, col150, col151, col152, col153, col154, col155, col156, col157, col158, col159, col160, col161, col162, col163, col164, col165, col166, col167, col168, col169, col170, col171, col172, col173, col174, col175, col176, col177, col178, col179, col180, col181, col182, col183, col184, col185, col186, col187, col188, col189, col190, col191, col192, col193, col194, col195, col196, col197, col198, col199, col200, col201, col202, col203,  ... col972, col973, col974, col975, col976, col977, col978, col979, col980, col981, col982, col983, col984, col985, col986, col987, col988, col989, col990, col991, col992, col993, col994, col995, col996, col997, col998, col999, WHERE Date=today()"
	start := crc.String()
	if start != expectedStart {
		t.Fatalf("unexpected query start read: (%d) %q; expecting (%d) %q", len(start), start, len(expectedStart), expectedStart)
	}
}

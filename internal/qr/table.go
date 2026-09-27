package qr

// rsSpec is the Reed-Solomon block layout of one symbol version at one
// error-correction level: how many blocks carry data1 bytes, how many carry
// data2 bytes (group 2 blocks always hold exactly one extra data codeword),
// and how many EC codewords are appended to every block.
// ISO/IEC 18004 Annex D, table 13-22.
type rsSpec struct {
	blocks1, data1 int
	blocks2, data2 int
	ecPerBlock     int
}

// rsBlockCount is the number of RS blocks in a symbol.
func (s rsSpec) rsBlockCount() int { return s.blocks1 + s.blocks2 }

// dataCodewords is the payload capacity of a symbol in codewords.
func (s rsSpec) dataCodewords() int { return s.blocks1*s.data1 + s.blocks2*s.data2 }

// totalCodewords is the codeword count of a symbol, EC included.
func (s rsSpec) totalCodewords() int {
	return s.rsBlockCount()*(s.data1+s.ecPerBlock) + s.blocks2*(s.data2-s.data1)
}

var rsTable = [4][41]rsSpec{
	{ // Low
		{0, 0, 0, 0, 0},       // v0 unused
		{1, 19, 0, 0, 7},      // v1  Low       19 data,   26 total
		{1, 34, 0, 0, 10},     // v2  Low       34 data,   44 total
		{1, 55, 0, 0, 15},     // v3  Low       55 data,   70 total
		{1, 80, 0, 0, 20},     // v4  Low       80 data,  100 total
		{1, 108, 0, 0, 26},    // v5  Low      108 data,  134 total
		{2, 68, 0, 0, 18},     // v6  Low      136 data,  172 total
		{2, 78, 0, 0, 20},     // v7  Low      156 data,  196 total
		{2, 97, 0, 0, 24},     // v8  Low      194 data,  242 total
		{2, 116, 0, 0, 30},    // v9  Low      232 data,  292 total
		{2, 68, 2, 69, 18},    // v10 Low      274 data,  346 total
		{4, 81, 0, 0, 20},     // v11 Low      324 data,  404 total
		{2, 92, 2, 93, 24},    // v12 Low      370 data,  466 total
		{4, 107, 0, 0, 26},    // v13 Low      428 data,  532 total
		{3, 115, 1, 116, 30},  // v14 Low      461 data,  581 total
		{5, 87, 1, 88, 22},    // v15 Low      523 data,  655 total
		{5, 98, 1, 99, 24},    // v16 Low      589 data,  733 total
		{1, 107, 5, 108, 28},  // v17 Low      647 data,  815 total
		{5, 120, 1, 121, 30},  // v18 Low      721 data,  901 total
		{3, 113, 4, 114, 28},  // v19 Low      795 data,  991 total
		{3, 107, 5, 108, 28},  // v20 Low      861 data, 1085 total
		{4, 116, 4, 117, 28},  // v21 Low      932 data, 1156 total
		{2, 111, 7, 112, 28},  // v22 Low     1006 data, 1258 total
		{4, 121, 5, 122, 30},  // v23 Low     1094 data, 1364 total
		{6, 117, 4, 118, 30},  // v24 Low     1174 data, 1474 total
		{8, 106, 4, 107, 26},  // v25 Low     1276 data, 1588 total
		{10, 114, 2, 115, 28}, // v26 Low     1370 data, 1706 total
		{8, 122, 4, 123, 30},  // v27 Low     1468 data, 1828 total
		{3, 117, 10, 118, 30}, // v28 Low     1531 data, 1921 total
		{7, 116, 7, 117, 30},  // v29 Low     1631 data, 2051 total
		{5, 115, 10, 116, 30}, // v30 Low     1735 data, 2185 total
		{13, 115, 3, 116, 30}, // v31 Low     1843 data, 2323 total
		{17, 115, 0, 0, 30},   // v32 Low     1955 data, 2465 total
		{17, 115, 1, 116, 30}, // v33 Low     2071 data, 2611 total
		{13, 115, 6, 116, 30}, // v34 Low     2191 data, 2761 total
		{12, 121, 7, 122, 30}, // v35 Low     2306 data, 2876 total
		{6, 121, 14, 122, 30}, // v36 Low     2434 data, 3034 total
		{17, 122, 4, 123, 30}, // v37 Low     2566 data, 3196 total
		{4, 122, 18, 123, 30}, // v38 Low     2702 data, 3362 total
		{20, 117, 4, 118, 30}, // v39 Low     2812 data, 3532 total
		{19, 118, 6, 119, 30}, // v40 Low     2956 data, 3706 total
	},
	{ // Medium
		{0, 0, 0, 0, 0},      // v0 unused
		{1, 16, 0, 0, 10},    // v1  Medium    16 data,   26 total
		{1, 28, 0, 0, 16},    // v2  Medium    28 data,   44 total
		{1, 44, 0, 0, 26},    // v3  Medium    44 data,   70 total
		{2, 32, 0, 0, 18},    // v4  Medium    64 data,  100 total
		{2, 43, 0, 0, 24},    // v5  Medium    86 data,  134 total
		{4, 27, 0, 0, 16},    // v6  Medium   108 data,  172 total
		{4, 31, 0, 0, 18},    // v7  Medium   124 data,  196 total
		{2, 38, 2, 39, 22},   // v8  Medium   154 data,  242 total
		{3, 36, 2, 37, 22},   // v9  Medium   182 data,  292 total
		{4, 43, 1, 44, 26},   // v10 Medium   216 data,  346 total
		{1, 50, 4, 51, 30},   // v11 Medium   254 data,  404 total
		{6, 36, 2, 37, 22},   // v12 Medium   290 data,  466 total
		{8, 37, 1, 38, 22},   // v13 Medium   334 data,  532 total
		{4, 40, 5, 41, 24},   // v14 Medium   365 data,  581 total
		{5, 41, 5, 42, 24},   // v15 Medium   415 data,  655 total
		{7, 45, 3, 46, 28},   // v16 Medium   453 data,  733 total
		{10, 46, 1, 47, 28},  // v17 Medium   507 data,  815 total
		{9, 43, 4, 44, 26},   // v18 Medium   563 data,  901 total
		{3, 44, 11, 45, 26},  // v19 Medium   627 data,  991 total
		{3, 41, 13, 42, 26},  // v20 Medium   669 data, 1085 total
		{17, 42, 0, 0, 26},   // v21 Medium   714 data, 1156 total
		{17, 46, 0, 0, 28},   // v22 Medium   782 data, 1258 total
		{4, 47, 14, 48, 28},  // v23 Medium   860 data, 1364 total
		{6, 45, 14, 46, 28},  // v24 Medium   914 data, 1474 total
		{8, 47, 13, 48, 28},  // v25 Medium  1000 data, 1588 total
		{19, 46, 4, 47, 28},  // v26 Medium  1062 data, 1706 total
		{22, 45, 3, 46, 28},  // v27 Medium  1128 data, 1828 total
		{3, 45, 23, 46, 28},  // v28 Medium  1193 data, 1921 total
		{21, 45, 7, 46, 28},  // v29 Medium  1267 data, 2051 total
		{19, 47, 10, 48, 28}, // v30 Medium  1373 data, 2185 total
		{2, 46, 29, 47, 28},  // v31 Medium  1455 data, 2323 total
		{10, 46, 23, 47, 28}, // v32 Medium  1541 data, 2465 total
		{14, 46, 21, 47, 28}, // v33 Medium  1631 data, 2611 total
		{14, 46, 23, 47, 28}, // v34 Medium  1725 data, 2761 total
		{12, 47, 26, 48, 28}, // v35 Medium  1812 data, 2876 total
		{6, 47, 34, 48, 28},  // v36 Medium  1914 data, 3034 total
		{29, 46, 14, 47, 28}, // v37 Medium  1992 data, 3196 total
		{13, 46, 32, 47, 28}, // v38 Medium  2102 data, 3362 total
		{40, 47, 7, 48, 28},  // v39 Medium  2216 data, 3532 total
		{18, 47, 31, 48, 28}, // v40 Medium  2334 data, 3706 total
	},
	{ // Quartile
		{0, 0, 0, 0, 0},      // v0 unused
		{1, 13, 0, 0, 13},    // v1  Quartile   13 data,   26 total
		{1, 22, 0, 0, 22},    // v2  Quartile   22 data,   44 total
		{2, 17, 0, 0, 18},    // v3  Quartile   34 data,   70 total
		{2, 24, 0, 0, 26},    // v4  Quartile   48 data,  100 total
		{2, 15, 2, 16, 18},   // v5  Quartile   62 data,  134 total
		{4, 19, 0, 0, 24},    // v6  Quartile   76 data,  172 total
		{2, 14, 4, 15, 18},   // v7  Quartile   88 data,  196 total
		{4, 18, 2, 19, 22},   // v8  Quartile  110 data,  242 total
		{4, 16, 4, 17, 20},   // v9  Quartile  132 data,  292 total
		{6, 19, 2, 20, 24},   // v10 Quartile  154 data,  346 total
		{4, 22, 4, 23, 28},   // v11 Quartile  180 data,  404 total
		{4, 20, 6, 21, 26},   // v12 Quartile  206 data,  466 total
		{8, 20, 4, 21, 24},   // v13 Quartile  244 data,  532 total
		{11, 16, 5, 17, 20},  // v14 Quartile  261 data,  581 total
		{5, 24, 7, 25, 30},   // v15 Quartile  295 data,  655 total
		{15, 19, 2, 20, 24},  // v16 Quartile  325 data,  733 total
		{1, 22, 15, 23, 28},  // v17 Quartile  367 data,  815 total
		{17, 22, 1, 23, 28},  // v18 Quartile  397 data,  901 total
		{17, 21, 4, 22, 26},  // v19 Quartile  445 data,  991 total
		{15, 24, 5, 25, 30},  // v20 Quartile  485 data, 1085 total
		{17, 22, 6, 23, 28},  // v21 Quartile  512 data, 1156 total
		{7, 24, 16, 25, 30},  // v22 Quartile  568 data, 1258 total
		{11, 24, 14, 25, 30}, // v23 Quartile  614 data, 1364 total
		{11, 24, 16, 25, 30}, // v24 Quartile  664 data, 1474 total
		{7, 24, 22, 25, 30},  // v25 Quartile  718 data, 1588 total
		{28, 22, 6, 23, 28},  // v26 Quartile  754 data, 1706 total
		{8, 23, 26, 24, 30},  // v27 Quartile  808 data, 1828 total
		{4, 24, 31, 25, 30},  // v28 Quartile  871 data, 1921 total
		{1, 23, 37, 24, 30},  // v29 Quartile  911 data, 2051 total
		{15, 24, 25, 25, 30}, // v30 Quartile  985 data, 2185 total
		{42, 24, 1, 25, 30},  // v31 Quartile 1033 data, 2323 total
		{10, 24, 35, 25, 30}, // v32 Quartile 1115 data, 2465 total
		{29, 24, 19, 25, 30}, // v33 Quartile 1171 data, 2611 total
		{44, 24, 7, 25, 30},  // v34 Quartile 1231 data, 2761 total
		{39, 24, 14, 25, 30}, // v35 Quartile 1286 data, 2876 total
		{46, 24, 10, 25, 30}, // v36 Quartile 1354 data, 3034 total
		{49, 24, 10, 25, 30}, // v37 Quartile 1426 data, 3196 total
		{48, 24, 14, 25, 30}, // v38 Quartile 1502 data, 3362 total
		{43, 24, 22, 25, 30}, // v39 Quartile 1582 data, 3532 total
		{34, 24, 34, 25, 30}, // v40 Quartile 1666 data, 3706 total
	},
	{ // High
		{0, 0, 0, 0, 0},      // v0 unused
		{1, 9, 0, 0, 17},     // v1  High       9 data,   26 total
		{1, 16, 0, 0, 28},    // v2  High      16 data,   44 total
		{2, 13, 0, 0, 22},    // v3  High      26 data,   70 total
		{4, 9, 0, 0, 16},     // v4  High      36 data,  100 total
		{2, 11, 2, 12, 22},   // v5  High      46 data,  134 total
		{4, 15, 0, 0, 28},    // v6  High      60 data,  172 total
		{4, 13, 1, 14, 26},   // v7  High      66 data,  196 total
		{4, 14, 2, 15, 26},   // v8  High      86 data,  242 total
		{4, 12, 4, 13, 24},   // v9  High     100 data,  292 total
		{6, 15, 2, 16, 28},   // v10 High     122 data,  346 total
		{3, 12, 8, 13, 24},   // v11 High     140 data,  404 total
		{7, 14, 4, 15, 28},   // v12 High     158 data,  466 total
		{12, 11, 4, 12, 22},  // v13 High     180 data,  532 total
		{11, 12, 5, 13, 24},  // v14 High     197 data,  581 total
		{11, 12, 7, 13, 24},  // v15 High     223 data,  655 total
		{3, 15, 13, 16, 30},  // v16 High     253 data,  733 total
		{2, 14, 17, 15, 28},  // v17 High     283 data,  815 total
		{2, 14, 19, 15, 28},  // v18 High     313 data,  901 total
		{9, 13, 16, 14, 26},  // v19 High     341 data,  991 total
		{15, 15, 10, 16, 28}, // v20 High     385 data, 1085 total
		{19, 16, 6, 17, 30},  // v21 High     406 data, 1156 total
		{34, 13, 0, 0, 24},   // v22 High     442 data, 1258 total
		{16, 15, 14, 16, 30}, // v23 High     464 data, 1364 total
		{30, 16, 2, 17, 30},  // v24 High     514 data, 1474 total
		{22, 15, 13, 16, 30}, // v25 High     538 data, 1588 total
		{33, 16, 4, 17, 30},  // v26 High     596 data, 1706 total
		{12, 15, 28, 16, 30}, // v27 High     628 data, 1828 total
		{11, 15, 31, 16, 30}, // v28 High     661 data, 1921 total
		{19, 15, 26, 16, 30}, // v29 High     701 data, 2051 total
		{23, 15, 25, 16, 30}, // v30 High     745 data, 2185 total
		{23, 15, 28, 16, 30}, // v31 High     793 data, 2323 total
		{19, 15, 35, 16, 30}, // v32 High     845 data, 2465 total
		{11, 15, 46, 16, 30}, // v33 High     901 data, 2611 total
		{59, 16, 1, 17, 30},  // v34 High     961 data, 2761 total
		{22, 15, 41, 16, 30}, // v35 High     986 data, 2876 total
		{2, 15, 64, 16, 30},  // v36 High    1054 data, 3034 total
		{24, 15, 46, 16, 30}, // v37 High    1096 data, 3196 total
		{42, 15, 32, 16, 30}, // v38 High    1142 data, 3362 total
		{10, 15, 67, 16, 30}, // v39 High    1222 data, 3532 total
		{20, 15, 61, 16, 30}, // v40 High    1276 data, 3706 total
	},
}

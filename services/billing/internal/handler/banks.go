package handler

import (
	"cmp"
	"slices"
)

const (
	// BankCatalogSource names the reviewed upstream snapshot without creating a runtime dependency.
	BankCatalogSource = "https://api.vietqr.io/v2/banks"
	// BankCatalogCapturedAt records when the reviewed source response was captured.
	BankCatalogCapturedAt = "2026-09-21T08:53:46Z"
	// BankCatalogSHA256 lets a future refresh compare the exact reviewed response.
	BankCatalogSHA256 = "788748dd264a7700d1e2702adcecf2e56dc8870b0f3b7cf742fa6c25bee8e70b"
)

// Bank is one stable payment network identity from the committed catalog.
type Bank struct {
	Code         string `json:"code"`
	ShortName    string `json:"short_name"`
	OfficialName string `json:"official_name"`
	Active       bool   `json:"-"`
}

// ActiveBanks returns the current selectable catalog in stable code order.
func ActiveBanks() []Bank {
	banks := bankSnapshot()
	active := make([]Bank, 0, len(banks))
	for _, bank := range banks {
		if bank.Active {
			active = append(active, bank)
		}
	}
	slices.SortFunc(active, func(left, right Bank) int { return cmp.Compare(left.Code, right.Code) })
	return active
}

// BankByCode returns active and retired entries so a saved code always keeps a stable label.
func BankByCode(code string) (Bank, bool) {
	for _, bank := range bankSnapshot() {
		if bank.Code == code {
			return bank, true
		}
	}
	return Bank{}, false
}

//nolint:funlen // A reviewed data snapshot stays auditable as one literal catalog.
func bankSnapshot() []Bank {
	return []Bank{
		{Code: "970415", ShortName: "VietinBank", OfficialName: "Ngân hàng TMCP Công thương Việt Nam", Active: true},
		{Code: "970436", ShortName: "Vietcombank", OfficialName: "Ngân hàng TMCP Ngoại Thương Việt Nam", Active: true},
		{Code: "970418", ShortName: "BIDV", OfficialName: "Ngân hàng TMCP Đầu tư và Phát triển Việt Nam", Active: true},
		{Code: "970405", ShortName: "Agribank", OfficialName: "Ngân hàng Nông nghiệp và Phát triển Nông thôn Việt Nam", Active: true},
		{Code: "970448", ShortName: "OCB", OfficialName: "Ngân hàng TMCP Phương Đông", Active: true},
		{Code: "970422", ShortName: "MBBank", OfficialName: "Ngân hàng TMCP Quân đội", Active: true},
		{Code: "970407", ShortName: "Techcombank", OfficialName: "Ngân hàng TMCP Kỹ thương Việt Nam", Active: true},
		{Code: "970416", ShortName: "ACB", OfficialName: "Ngân hàng TMCP Á Châu", Active: true},
		{Code: "970432", ShortName: "VPBank", OfficialName: "Ngân hàng TMCP Việt Nam Thịnh Vượng", Active: true},
		{Code: "970423", ShortName: "TPBank", OfficialName: "Ngân hàng TMCP Tiên Phong", Active: true},
		{Code: "970403", ShortName: "Sacombank", OfficialName: "Ngân hàng TMCP Sài Gòn Thương Tín", Active: true},
		{Code: "970437", ShortName: "HDBank", OfficialName: "Ngân hàng TMCP Phát triển Thành phố Hồ Chí Minh", Active: true},
		{Code: "970454", ShortName: "VietCapitalBank", OfficialName: "Ngân hàng TMCP Bản Việt", Active: true},
		{Code: "970429", ShortName: "SCB", OfficialName: "Ngân hàng TMCP Sài Gòn", Active: true},
		{Code: "970441", ShortName: "VIB", OfficialName: "Ngân hàng TMCP Quốc tế Việt Nam", Active: true},
		{Code: "970443", ShortName: "SHB", OfficialName: "Ngân hàng TMCP Sài Gòn - Hà Nội", Active: true},
		{Code: "970431", ShortName: "Eximbank", OfficialName: "Ngân hàng TMCP Xuất Nhập khẩu Việt Nam", Active: true},
		{Code: "970426", ShortName: "MSB", OfficialName: "Ngân hàng TMCP Hàng Hải Việt Nam", Active: true},
		{Code: "546034", ShortName: "CAKE", OfficialName: "TMCP Việt Nam Thịnh Vượng - Ngân hàng số CAKE by VPBank", Active: true},
		{Code: "546035", ShortName: "Ubank", OfficialName: "TMCP Việt Nam Thịnh Vượng - Ngân hàng số Ubank by VPBank", Active: true},
		{Code: "971005", ShortName: "ViettelMoney", OfficialName: "Tổng Công ty Dịch vụ số Viettel - Chi nhánh tập đoàn công nghiệp viễn thông Quân Đội", Active: false},
		{Code: "963388", ShortName: "Timo", OfficialName: "Ngân hàng số Timo by Ban Viet Bank (Timo by Ban Viet Bank)", Active: true},
		{Code: "971011", ShortName: "VNPTMoney", OfficialName: "VNPT Money", Active: false},
		{Code: "970400", ShortName: "SaigonBank", OfficialName: "Ngân hàng TMCP Sài Gòn Công Thương", Active: true},
		{Code: "970409", ShortName: "BacABank", OfficialName: "Ngân hàng TMCP Bắc Á", Active: true},
		{Code: "971025", ShortName: "MoMo", OfficialName: "CTCP Dịch Vụ Di Động Trực Tuyến", Active: true},
		{Code: "971133", ShortName: "PVcomBank Pay", OfficialName: "Ngân hàng TMCP Đại Chúng Việt Nam Ngân hàng số", Active: true},
		{Code: "970412", ShortName: "PVcomBank", OfficialName: "Ngân hàng TMCP Đại Chúng Việt Nam", Active: true},
		{Code: "970414", ShortName: "MBV", OfficialName: "Ngân hàng TNHH MTV Việt Nam Hiện Đại", Active: true},
		{Code: "970419", ShortName: "NCB", OfficialName: "Ngân hàng TMCP Quốc Dân", Active: true},
		{Code: "970424", ShortName: "ShinhanBank", OfficialName: "Ngân hàng TNHH MTV Shinhan Việt Nam", Active: true},
		{Code: "970425", ShortName: "ABBANK", OfficialName: "Ngân hàng TMCP An Bình", Active: true},
		{Code: "970427", ShortName: "VietABank", OfficialName: "Ngân hàng TMCP Việt Á", Active: true},
		{Code: "970428", ShortName: "NamABank", OfficialName: "Ngân hàng TMCP Nam Á", Active: true},
		{Code: "970430", ShortName: "PGBank", OfficialName: "Ngân hàng TMCP Thịnh vượng và Phát triển", Active: true},
		{Code: "970433", ShortName: "VietBank", OfficialName: "Ngân hàng TMCP Việt Nam Thương Tín", Active: true},
		{Code: "970438", ShortName: "BaoVietBank", OfficialName: "Ngân hàng TMCP Bảo Việt", Active: true},
		{Code: "970440", ShortName: "SeABank", OfficialName: "Ngân hàng TMCP Đông Nam Á", Active: true},
		{Code: "970446", ShortName: "COOPBANK", OfficialName: "Ngân hàng Hợp tác xã Việt Nam", Active: true},
		{Code: "970449", ShortName: "LPBank", OfficialName: "Ngân hàng TMCP Lộc Phát Việt Nam", Active: true},
		{Code: "970452", ShortName: "KienLongBank", OfficialName: "Ngân hàng TMCP Kiên Long", Active: true},
		{Code: "668888", ShortName: "KBank", OfficialName: "Ngân hàng Đại chúng TNHH Kasikornbank", Active: true},
		{Code: "977777", ShortName: "MAFC", OfficialName: "Công ty Tài chính TNHH MTV Mirae Asset (Việt Nam) ", Active: false},
		{Code: "970442", ShortName: "HongLeong", OfficialName: "Ngân hàng TNHH MTV Hong Leong Việt Nam", Active: false},
		{Code: "970467", ShortName: "KEBHANAHN", OfficialName: "Ngân hàng KEB Hana – Chi nhánh Hà Nội", Active: false},
		{Code: "970466", ShortName: "KEBHanaHCM", OfficialName: "Ngân hàng KEB Hana – Chi nhánh Thành phố Hồ Chí Minh", Active: false},
		{Code: "533948", ShortName: "Citibank", OfficialName: "Ngân hàng Citibank, N.A. - Chi nhánh Hà Nội", Active: false},
		{Code: "970444", ShortName: "CBBank", OfficialName: "Ngân hàng Thương mại TNHH MTV Xây dựng Việt Nam", Active: false},
		{Code: "422589", ShortName: "CIMB", OfficialName: "Ngân hàng TNHH MTV CIMB Việt Nam", Active: true},
		{Code: "796500", ShortName: "DBSBank", OfficialName: "DBS Bank Ltd - Chi nhánh Thành phố Hồ Chí Minh", Active: false},
		{Code: "970406", ShortName: "Vikki", OfficialName: "Ngân hàng TNHH MTV Số Vikki", Active: false},
		{Code: "999888", ShortName: "VBSP", OfficialName: "Ngân hàng Chính sách Xã hội", Active: false},
		{Code: "970408", ShortName: "GPBank", OfficialName: "Ngân hàng Thương mại TNHH MTV Dầu Khí Toàn Cầu", Active: false},
		{Code: "970463", ShortName: "KookminHCM", OfficialName: "Ngân hàng Kookmin - Chi nhánh Thành phố Hồ Chí Minh", Active: false},
		{Code: "970462", ShortName: "KookminHN", OfficialName: "Ngân hàng Kookmin - Chi nhánh Hà Nội", Active: false},
		{Code: "970457", ShortName: "Woori", OfficialName: "Ngân hàng TNHH MTV Woori Việt Nam", Active: true},
		{Code: "970421", ShortName: "VRB", OfficialName: "Ngân hàng Liên doanh Việt - Nga", Active: false},
		{Code: "458761", ShortName: "HSBC", OfficialName: "Ngân hàng TNHH MTV HSBC (Việt Nam)", Active: false},
		{Code: "970455", ShortName: "IBKHN", OfficialName: "Ngân hàng Công nghiệp Hàn Quốc - Chi nhánh Hà Nội", Active: false},
		{Code: "970456", ShortName: "IBKHCM", OfficialName: "Ngân hàng Công nghiệp Hàn Quốc - Chi nhánh TP. Hồ Chí Minh", Active: false},
		{Code: "970434", ShortName: "IndovinaBank", OfficialName: "Ngân hàng TNHH Indovina", Active: false},
		{Code: "970458", ShortName: "UnitedOverseas", OfficialName: "Ngân hàng United Overseas - Chi nhánh TP. Hồ Chí Minh", Active: false},
		{Code: "801011", ShortName: "Nonghyup", OfficialName: "Ngân hàng Nonghyup - Chi nhánh Hà Nội", Active: false},
		{Code: "970410", ShortName: "StandardChartered", OfficialName: "Ngân hàng TNHH MTV Standard Chartered Bank Việt Nam", Active: false},
		{Code: "970439", ShortName: "PublicBank", OfficialName: "Ngân hàng TNHH MTV Public Việt Nam", Active: false},
	}
}

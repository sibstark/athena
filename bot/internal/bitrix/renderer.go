package bitrix

type LinkRenderer struct{}

func (LinkRenderer) Link(label string, target string) string {
	return "[url=" + target + "]" + label + "[/url]"
}

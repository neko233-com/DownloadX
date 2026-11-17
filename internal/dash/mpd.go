// Package dash parses static MPEG-DASH manifests and downloads them.
package dash

import (
	"encoding/xml"
	"errors"
	"fmt"
	"math"
	"net/url"
	"regexp"
	"strconv"
	"strings"
)

// MPD is a parsed Media Presentation Description.
type MPD struct {
	XMLName                   xml.Name `xml:"MPD"`
	Type                      string   `xml:"type,attr"`
	MediaPresentationDuration string   `xml:"mediaPresentationDuration,attr"`
	BaseURL                   string   `xml:"BaseURL"`
	Periods                   []Period `xml:"Period"`

	URL string `xml:"-"` // source URL, absolute
}

// Period is one MPD period. Only the first period is downloaded.
type Period struct {
	Duration        string           `xml:"duration,attr"`
	BaseURL         string           `xml:"BaseURL"`
	SegmentTemplate *SegmentTemplate `xml:"SegmentTemplate"`
	SegmentList     *SegmentList     `xml:"SegmentList"`
	SegmentBase     *SegmentBase     `xml:"SegmentBase"`
	AdaptationSets  []AdaptationSet  `xml:"AdaptationSet"`
}

// AdaptationSet groups representations that share parameters.
type AdaptationSet struct {
	ContentType       string              `xml:"contentType,attr"`
	MimeType          string              `xml:"mimeType,attr"`
	Lang              string              `xml:"lang,attr"`
	BaseURL           string              `xml:"BaseURL"`
	SegmentTemplate   *SegmentTemplate    `xml:"SegmentTemplate"`
	SegmentList       *SegmentList        `xml:"SegmentList"`
	SegmentBase       *SegmentBase        `xml:"SegmentBase"`
	Representations   []Representation    `xml:"Representation"`
	ContentProtection []ContentProtection `xml:"ContentProtection"`
}

// Representation is one audio or video encoding.
type Representation struct {
	ID                string              `xml:"id,attr"`
	Bandwidth         int64               `xml:"bandwidth,attr"`
	Codecs            string              `xml:"codecs,attr"`
	Width             int                 `xml:"width,attr"`
	Height            int                 `xml:"height,attr"`
	MimeType          string              `xml:"mimeType,attr"`
	BaseURL           string              `xml:"BaseURL"`
	SegmentTemplate   *SegmentTemplate    `xml:"SegmentTemplate"`
	SegmentList       *SegmentList        `xml:"SegmentList"`
	SegmentBase       *SegmentBase        `xml:"SegmentBase"`
	ContentProtection []ContentProtection `xml:"ContentProtection"`
}

// SegmentTemplate addresses segments with URL templates.
type SegmentTemplate struct {
	Media                  string           `xml:"media,attr"`
	Initialization         string           `xml:"initialization,attr"`
	Timescale              int64            `xml:"timescale,attr"`
	Duration               int64            `xml:"duration,attr"`
	StartNumber            *int64           `xml:"startNumber,attr"`
	PresentationTimeOffset int64            `xml:"presentationTimeOffset,attr"`
	SegmentTimeline        *SegmentTimeline `xml:"SegmentTimeline"`
}

// SegmentTimeline lists explicit segment start times and durations.
type SegmentTimeline struct {
	S []S `xml:"S"`
}

// S is one SegmentTimeline entry.
type S struct {
	T *int64 `xml:"t,attr"`
	D int64  `xml:"d,attr"`
	R int64  `xml:"r,attr"`
}

// SegmentList lists complete segment URLs.
type SegmentList struct {
	Initialization *URLType     `xml:"Initialization"`
	SegmentURLs    []SegmentURL `xml:"SegmentURL"`
}

// SegmentURL is one entry of a SegmentList.
type SegmentURL struct {
	Media      string `xml:"media,attr"`
	MediaRange string `xml:"mediaRange,attr"`
}

// URLType references an initialization resource.
type URLType struct {
	SourceURL string `xml:"sourceURL,attr"`
	Range     string `xml:"range,attr"`
}

// SegmentBase describes a single-file representation.
type SegmentBase struct {
	Initialization *URLType `xml:"Initialization"`
}

// ContentProtection marks DRM.
type ContentProtection struct {
	SchemeIDURI string `xml:"schemeIdUri,attr"`
	Value       string `xml:"value,attr"`
}

// Track is one resolved audio or video representation.
type Track struct {
	Kind        string
	ID          string
	MimeType    string
	Codecs      string
	Width       int
	Height      int
	Bandwidth   int64
	Base        *url.URL
	Duration    float64
	Template    *SegmentTemplate
	SegmentList *SegmentList
	SegmentBase *SegmentBase
	Protected   bool
}

// ByteRange is an inclusive byte range.
type ByteRange struct {
	Start int64
	End   int64
}

// SegmentRef is one URL to fetch.
type SegmentRef struct {
	URL   string
	Range *ByteRange
}

// Plan is the ordered download plan for one track.
type Plan struct {
	Init     *SegmentRef
	Segments []SegmentRef
}

// Parse parses an MPD document. Dynamic (live) manifests are rejected.
func Parse(mpdURL string, data []byte) (*MPD, error) {
	var m MPD
	if err := xml.Unmarshal(data, &m); err != nil {
		return nil, fmt.Errorf("dash: parse MPD: %w", err)
	}
	m.URL = mpdURL
	if strings.EqualFold(m.Type, "dynamic") {
		return nil, errors.New("dash: dynamic (live) MPDs are not supported")
	}
	if len(m.Periods) == 0 {
		return nil, errors.New("dash: MPD has no Period")
	}
	return &m, nil
}

// Tracks resolves all audio/video representations in the first period.
func (m *MPD) Tracks() ([]*Track, error) {
	if len(m.Periods) == 0 {
		return nil, errors.New("dash: MPD has no Period")
	}
	mpdBase, err := url.Parse(m.URL)
	if err != nil {
		return nil, fmt.Errorf("dash: bad MPD URL: %w", err)
	}
	p := &m.Periods[0]

	duration := parseDuration(p.Duration)
	if duration <= 0 {
		duration = parseDuration(m.MediaPresentationDuration)
	}

	periodBase := resolveBase(mpdBase, m.BaseURL)
	periodBase = resolveBase(periodBase, p.BaseURL)

	var tracks []*Track
	for i := range p.AdaptationSets {
		as := &p.AdaptationSets[i]
		asBase := resolveBase(periodBase, as.BaseURL)
		for j := range as.Representations {
			rep := &as.Representations[j]
			kind := strings.ToLower(strings.TrimSpace(as.ContentType))
			if kind == "" {
				kind = mimeKind(as.MimeType)
			}
			if kind == "" {
				kind = mimeKind(rep.MimeType)
			}
			if kind != "video" && kind != "audio" {
				continue
			}
			base := resolveBase(asBase, rep.BaseURL)
			tr := &Track{
				Kind:        kind,
				ID:          rep.ID,
				MimeType:    firstNonEmpty(rep.MimeType, as.MimeType),
				Codecs:      rep.Codecs,
				Width:       rep.Width,
				Height:      rep.Height,
				Bandwidth:   rep.Bandwidth,
				Base:        base,
				Duration:    duration,
				Template:    firstTemplate(rep.SegmentTemplate, as.SegmentTemplate, p.SegmentTemplate),
				SegmentList: firstList(rep.SegmentList, as.SegmentList, p.SegmentList),
				SegmentBase: firstBase(rep.SegmentBase, as.SegmentBase, p.SegmentBase),
				Protected:   len(as.ContentProtection) > 0 || len(rep.ContentProtection) > 0,
			}
			tracks = append(tracks, tr)
		}
	}
	return tracks, nil
}

// PickTracks selects the highest-bandwidth video and audio representations.
// DRM-protected manifests are refused.
func (m *MPD) PickTracks() (video, audio *Track, err error) {
	tracks, err := m.Tracks()
	if err != nil {
		return nil, nil, err
	}
	for _, tr := range tracks {
		if tr.Protected {
			return nil, nil, errors.New("dash: DRM-protected content (ContentProtection) is not supported")
		}
	}
	for _, tr := range tracks {
		switch tr.Kind {
		case "video":
			if video == nil || tr.Bandwidth > video.Bandwidth {
				video = tr
			}
		case "audio":
			if audio == nil || tr.Bandwidth > audio.Bandwidth {
				audio = tr
			}
		}
	}
	if video == nil && audio == nil {
		return nil, nil, errors.New("dash: no audio or video representations")
	}
	return video, audio, nil
}

// Plan resolves a track into initialization and media segment URLs.
func (tr *Track) Plan() (*Plan, error) {
	switch {
	case tr.Template != nil && tr.Template.Media != "":
		return tr.planTemplate()
	case tr.SegmentList != nil:
		return tr.planList()
	default:
		return tr.planBase()
	}
}

func (tr *Track) planTemplate() (*Plan, error) {
	tpl := tr.Template
	if tr.Base == nil {
		return nil, errors.New("dash: representation has no base URL")
	}
	timescale := tpl.Timescale
	if timescale <= 0 {
		timescale = 1
	}
	startNumber := int64(1)
	if tpl.StartNumber != nil {
		startNumber = *tpl.StartNumber
	}

	plan := &Plan{}
	if tpl.Initialization != "" {
		ref, err := tr.expandRef(tpl.Initialization, startNumber, 0)
		if err != nil {
			return nil, err
		}
		plan.Init = &SegmentRef{URL: ref}
	}

	appendSeg := func(number, tm int64) error {
		ref, err := tr.expandRef(tpl.Media, number, tm)
		if err != nil {
			return err
		}
		plan.Segments = append(plan.Segments, SegmentRef{URL: ref})
		return nil
	}

	if tpl.SegmentTimeline != nil && len(tpl.SegmentTimeline.S) > 0 {
		timeCursor := int64(0)
		number := startNumber
		for _, s := range tpl.SegmentTimeline.S {
			if s.T != nil {
				timeCursor = *s.T
			}
			if s.D <= 0 {
				return nil, errors.New("dash: SegmentTimeline has non-positive duration")
			}
			if s.R < 0 {
				for {
					if err := appendSeg(number, timeCursor); err != nil {
						return nil, err
					}
					timeCursor += s.D
					number++
					if tr.Duration <= 0 || float64(timeCursor)/float64(timescale) >= tr.Duration {
						break
					}
				}
				continue
			}
			for rep := int64(0); rep <= s.R; rep++ {
				if err := appendSeg(number, timeCursor); err != nil {
					return nil, err
				}
				timeCursor += s.D
				number++
			}
		}
		if len(plan.Segments) == 0 {
			return nil, errors.New("dash: empty SegmentTimeline")
		}
		return plan, nil
	}

	if tpl.Duration <= 0 {
		return nil, errors.New("dash: SegmentTemplate has neither duration nor SegmentTimeline")
	}
	if tr.Duration <= 0 {
		return nil, errors.New("dash: MPD/Period duration missing; cannot size SegmentTemplate")
	}
	count := int(math.Ceil(tr.Duration * float64(timescale) / float64(tpl.Duration)))
	if count < 1 {
		count = 1
	}
	for i := 0; i < count; i++ {
		number := startNumber + int64(i)
		tm := tpl.PresentationTimeOffset + int64(i)*tpl.Duration
		if err := appendSeg(number, tm); err != nil {
			return nil, err
		}
	}
	return plan, nil
}

func (tr *Track) planList() (*Plan, error) {
	list := tr.SegmentList
	plan := &Plan{}
	if list.Initialization != nil && list.Initialization.SourceURL != "" {
		u, err := resolveRef(tr.Base, list.Initialization.SourceURL)
		if err != nil {
			return nil, err
		}
		plan.Init = &SegmentRef{URL: u, Range: parseRange(list.Initialization.Range)}
	}
	for _, su := range list.SegmentURLs {
		if su.Media == "" {
			return nil, errors.New("dash: SegmentList entry without media URL")
		}
		u, err := resolveRef(tr.Base, su.Media)
		if err != nil {
			return nil, err
		}
		plan.Segments = append(plan.Segments, SegmentRef{URL: u, Range: parseRange(su.MediaRange)})
	}
	if len(plan.Segments) == 0 {
		return nil, errors.New("dash: SegmentList has no segment URLs")
	}
	return plan, nil
}

func (tr *Track) planBase() (*Plan, error) {
	if tr.Base == nil {
		return nil, errors.New("dash: representation has no base URL")
	}
	plan := &Plan{}
	if tr.SegmentBase != nil && tr.SegmentBase.Initialization != nil && tr.SegmentBase.Initialization.SourceURL != "" {
		u, err := resolveRef(tr.Base, tr.SegmentBase.Initialization.SourceURL)
		if err != nil {
			return nil, err
		}
		plan.Init = &SegmentRef{URL: u, Range: parseRange(tr.SegmentBase.Initialization.Range)}
	}
	plan.Segments = append(plan.Segments, SegmentRef{URL: tr.Base.String()})
	return plan, nil
}

func (tr *Track) expandRef(template string, number, tm int64) (string, error) {
	expanded, err := expandTemplate(template, tr, number, tm)
	if err != nil {
		return "", err
	}
	return resolveRef(tr.Base, expanded)
}

var numberFormat = regexp.MustCompile(`^%0?[0-9]*d$`)

func expandTemplate(tpl string, tr *Track, number, tm int64) (string, error) {
	var b strings.Builder
	for i := 0; i < len(tpl); {
		if tpl[i] != '$' {
			b.WriteByte(tpl[i])
			i++
			continue
		}
		if i+1 < len(tpl) && tpl[i+1] == '$' {
			b.WriteByte('$')
			i += 2
			continue
		}
		j := strings.IndexByte(tpl[i+1:], '$')
		if j < 0 {
			return "", errors.New("dash: unmatched $ in URL template")
		}
		token := tpl[i+1 : i+1+j]
		i += j + 2

		name, format := token, ""
		if k := strings.IndexByte(token, '%'); k >= 0 {
			name, format = token[:k], token[k:]
		}
		switch name {
		case "RepresentationID":
			if format != "" {
				return "", fmt.Errorf("dash: format is not valid for $RepresentationID$")
			}
			b.WriteString(tr.ID)
		case "Bandwidth", "Number", "Time":
			var v int64
			switch name {
			case "Bandwidth":
				v = tr.Bandwidth
			case "Number":
				v = number
			case "Time":
				v = tm
			}
			if format == "" {
				b.WriteString(strconv.FormatInt(v, 10))
				continue
			}
			if !numberFormat.MatchString(format) {
				return "", fmt.Errorf("dash: unsupported template format %q", format)
			}
			b.WriteString(fmt.Sprintf(format, v))
		default:
			return "", fmt.Errorf("dash: unknown template variable $%s$", name)
		}
	}
	return b.String(), nil
}

var isoDuration = regexp.MustCompile(`^P(?:(\d+)D)?(?:T(?:(\d+)H)?(?:(\d+)M)?(?:(\d+(?:\.\d+)?)S)?)?$`)

func parseDuration(s string) float64 {
	s = strings.TrimSpace(strings.ToUpper(s))
	if s == "" {
		return 0
	}
	m := isoDuration.FindStringSubmatch(s)
	if m == nil {
		return 0
	}
	var seconds float64
	if m[1] != "" {
		v, _ := strconv.ParseFloat(m[1], 64)
		seconds += v * 86400
	}
	if m[2] != "" {
		v, _ := strconv.ParseFloat(m[2], 64)
		seconds += v * 3600
	}
	if m[3] != "" {
		v, _ := strconv.ParseFloat(m[3], 64)
		seconds += v * 60
	}
	if m[4] != "" {
		v, _ := strconv.ParseFloat(m[4], 64)
		seconds += v
	}
	return seconds
}

func parseRange(s string) *ByteRange {
	s = strings.TrimSpace(s)
	if s == "" {
		return nil
	}
	a, b, ok := strings.Cut(s, "-")
	if !ok {
		return nil
	}
	start, err1 := strconv.ParseInt(strings.TrimSpace(a), 10, 64)
	end, err2 := strconv.ParseInt(strings.TrimSpace(b), 10, 64)
	if err1 != nil || err2 != nil || start < 0 || end < start {
		return nil
	}
	return &ByteRange{Start: start, End: end}
}

func resolveBase(parent *url.URL, ref string) *url.URL {
	if ref == "" {
		return parent
	}
	u, err := url.Parse(ref)
	if err != nil {
		return parent
	}
	return parent.ResolveReference(u)
}

func resolveRef(base *url.URL, ref string) (string, error) {
	if base == nil {
		return "", errors.New("dash: missing base URL")
	}
	u, err := url.Parse(ref)
	if err != nil {
		return "", fmt.Errorf("dash: bad segment URL %q: %w", ref, err)
	}
	return base.ResolveReference(u).String(), nil
}

func mimeKind(mime string) string {
	mime = strings.ToLower(strings.TrimSpace(mime))
	if i := strings.IndexByte(mime, '/'); i > 0 {
		mime = mime[:i]
	}
	switch mime {
	case "video", "audio":
		return mime
	}
	return ""
}

func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if v != "" {
			return v
		}
	}
	return ""
}

func firstTemplate(values ...*SegmentTemplate) *SegmentTemplate {
	for _, v := range values {
		if v != nil {
			return v
		}
	}
	return nil
}

func firstList(values ...*SegmentList) *SegmentList {
	for _, v := range values {
		if v != nil {
			return v
		}
	}
	return nil
}

func firstBase(values ...*SegmentBase) *SegmentBase {
	for _, v := range values {
		if v != nil {
			return v
		}
	}
	return nil
}

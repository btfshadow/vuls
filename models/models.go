/* Vuls - Vulnerability Scanner
Copyright (C) 2016  Future Architect, Inc. Japan.

This program is free software: you can redistribute it and/or modify
it under the terms of the GNU General Public License as published by
the Free Software Foundation, either version 3 of the License, or
(at your option) any later version.

This program is distributed in the hope that it will be useful,
but WITHOUT ANY WARRANTY; without even the implied warranty of
MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE.  See the
GNU General Public License for more details.

You should have received a copy of the GNU General Public License
along with this program.  If not, see <http://www.gnu.org/licenses/>.
*/

package models

import (
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/future-architect/vuls/config"
	"github.com/future-architect/vuls/cveapi"
	cvedict "github.com/kotakanbe/go-cve-dictionary/models"
)

// ScanResults is slice of ScanResult.
type ScanResults []ScanResult

// Len implement Sort Interface
func (s ScanResults) Len() int {
	return len(s)
}

// Swap implement Sort Interface
func (s ScanResults) Swap(i, j int) {
	s[i], s[j] = s[j], s[i]
}

// Less implement Sort Interface
func (s ScanResults) Less(i, j int) bool {
	if s[i].ServerName == s[j].ServerName {
		return s[i].Container.ContainerID < s[i].Container.ContainerID
	}
	return s[i].ServerName < s[j].ServerName
}

// ScanResult has the result of scanned CVE information.
type ScanResult struct {
	ScannedAt time.Time

	Lang       string
	ServerName string // TOML Section key
	Family     string
	Release    string
	Container  Container
	Platform   Platform

	// Scanned Vulns by SSH scan + CPE + OVAL
	ScannedCves VulnInfos

	Packages PackageInfoList
	Errors   []string
	Optional [][]interface{}
}

// FillCveDetail fetches NVD, JVN from CVE Database, and then set to fields.
//TODO rename to FillCveDictionary
func (r ScanResult) FillCveDetail() (*ScanResult, error) {
	var cveIDs []string
	for _, v := range r.ScannedCves {
		cveIDs = append(cveIDs, v.CveID)
	}

	ds, err := cveapi.CveClient.FetchCveDetails(cveIDs)
	if err != nil {
		return nil, err
	}
	for _, d := range ds {
		nvd := *r.convertNvdToModel(d.CveID, d.Nvd)
		jvn := *r.convertJvnToModel(d.CveID, d.Jvn)
		for i, sc := range r.ScannedCves {
			if sc.CveID == d.CveID {
				for _, con := range []CveContent{nvd, jvn} {
					if !con.Empty() {
						r.ScannedCves[i].CveContents.Upsert(con)
					}
				}
				break
			}
		}
	}
	//TODO sort
	//  sort.Sort(r.KnownCves)
	//  sort.Sort(r.UnknownCves)
	//  sort.Sort(r.IgnoredCves)
	return &r, nil
}

func (r ScanResult) convertNvdToModel(cveID string, nvd cvedict.Nvd) *CveContent {
	var cpes []Cpe
	for _, c := range nvd.Cpes {
		cpes = append(cpes, Cpe{CpeName: c.CpeName})
	}

	var refs []Reference
	for _, r := range nvd.References {
		refs = append(refs, Reference{
			Link:   r.Link,
			Source: r.Source,
		})
	}

	validVec := true
	for _, v := range []string{
		nvd.AccessVector,
		nvd.AccessComplexity,
		nvd.Authentication,
		nvd.ConfidentialityImpact,
		nvd.IntegrityImpact,
		nvd.AvailabilityImpact,
	} {
		if len(v) == 0 {
			validVec = false
		}
	}

	vector := ""
	if validVec {
		vector = fmt.Sprintf("AV:%s/AC:%s/Au:%s/C:%s/I:%s/A:%s",
			string(nvd.AccessVector[0]),
			string(nvd.AccessComplexity[0]),
			string(nvd.Authentication[0]),
			string(nvd.ConfidentialityImpact[0]),
			string(nvd.IntegrityImpact[0]),
			string(nvd.AvailabilityImpact[0]))
	}

	//TODO CVSSv3
	return &CveContent{
		Type:         NVD,
		CveID:        cveID,
		Summary:      nvd.Summary,
		Cvss2Score:   nvd.Score,
		Cvss2Vector:  vector,
		Cpes:         cpes,
		CweID:        nvd.CweID,
		References:   refs,
		Published:    nvd.PublishedDate,
		LastModified: nvd.LastModifiedDate,
	}
}

func (r ScanResult) convertJvnToModel(cveID string, jvn cvedict.Jvn) *CveContent {
	var cpes []Cpe
	for _, c := range jvn.Cpes {
		cpes = append(cpes, Cpe{CpeName: c.CpeName})
	}

	refs := []Reference{{
		Link:   jvn.JvnLink,
		Source: string(JVN),
	}}
	for _, r := range jvn.References {
		refs = append(refs, Reference{
			Link:   r.Link,
			Source: r.Source,
		})
	}

	vector := strings.TrimSuffix(strings.TrimPrefix(jvn.Vector, "("), ")")
	return &CveContent{
		Type:         JVN,
		CveID:        cveID,
		Title:        jvn.Title,
		Summary:      jvn.Summary,
		Severity:     jvn.Severity,
		Cvss2Score:   jvn.Score,
		Cvss2Vector:  vector,
		Cpes:         cpes,
		References:   refs,
		Published:    jvn.PublishedDate,
		LastModified: jvn.LastModifiedDate,
	}
}

// FilterByCvssOver is filter function.
func (r ScanResult) FilterByCvssOver() ScanResult {
	// TODO: Set correct default value
	if config.Conf.CvssScoreOver == 0 {
		config.Conf.CvssScoreOver = -1.1
	}

	// TODO: Filter by ignore cves???
	filtered := VulnInfos{}
	for _, sc := range r.ScannedCves {
		if config.Conf.CvssScoreOver <= sc.CveContents.CvssV2Score() {
			filtered = append(filtered, sc)
		}
	}
	copiedScanResult := r
	copiedScanResult.ScannedCves = filtered
	return copiedScanResult
}

// ReportFileName returns the filename on localhost without extention
func (r ScanResult) ReportFileName() (name string) {
	if len(r.Container.ContainerID) == 0 {
		return fmt.Sprintf("%s", r.ServerName)
	}
	return fmt.Sprintf("%s@%s", r.Container.Name, r.ServerName)
}

// ReportKeyName returns the name of key on S3, Azure-Blob without extention
func (r ScanResult) ReportKeyName() (name string) {
	timestr := r.ScannedAt.Format(time.RFC3339)
	if len(r.Container.ContainerID) == 0 {
		return fmt.Sprintf("%s/%s", timestr, r.ServerName)
	}
	return fmt.Sprintf("%s/%s@%s", timestr, r.Container.Name, r.ServerName)
}

// ServerInfo returns server name one line
func (r ScanResult) ServerInfo() string {
	if len(r.Container.ContainerID) == 0 {
		return fmt.Sprintf("%s (%s%s)",
			r.ServerName, r.Family, r.Release)
	}
	return fmt.Sprintf(
		"%s / %s (%s%s) on %s",
		r.Container.Name,
		r.Container.ContainerID,
		r.Family,
		r.Release,
		r.ServerName,
	)
}

// ServerInfoTui returns server infromation for TUI sidebar
func (r ScanResult) ServerInfoTui() string {
	if len(r.Container.ContainerID) == 0 {
		return fmt.Sprintf("%s (%s%s)",
			r.ServerName, r.Family, r.Release)
	}
	return fmt.Sprintf(
		"|-- %s (%s%s)",
		r.Container.Name,
		r.Family,
		r.Release,
		//  r.Container.ContainerID,
	)
}

// FormatServerName returns server and container name
func (r ScanResult) FormatServerName() string {
	if len(r.Container.ContainerID) == 0 {
		return r.ServerName
	}
	return fmt.Sprintf("%s@%s",
		r.Container.Name, r.ServerName)
}

// CveSummary summarize the number of CVEs group by CVSSv2 Severity
func (r ScanResult) CveSummary() string {
	var high, medium, low, unknown int
	for _, vInfo := range r.ScannedCves {
		score := vInfo.CveContents.CvssV2Score()
		switch {
		case 7.0 <= score:
			high++
		case 4.0 <= score:
			medium++
		case 0 < score:
			low++
		default:
			unknown++
		}
	}

	if config.Conf.IgnoreUnscoredCves {
		return fmt.Sprintf("Total: %d (High:%d Medium:%d Low:%d)",
			high+medium+low, high, medium, low)
	}
	return fmt.Sprintf("Total: %d (High:%d Medium:%d Low:%d ?:%d)",
		high+medium+low+unknown, high, medium, low, unknown)
}

// NWLink has network link information.
//TODO remove
//  type NWLink struct {
//      IPAddress string
//      Netmask   string
//      DevName   string
//      LinkState string
//  }

// Confidence is a ranking how confident the CVE-ID was deteted correctly
// Score: 0 - 100
type Confidence struct {
	Score           int
	DetectionMethod string
}

func (c Confidence) String() string {
	return fmt.Sprintf("%d / %s", c.Score, c.DetectionMethod)
}

const (
	// CpeNameMatchStr is a String representation of CpeNameMatch
	CpeNameMatchStr = "CpeNameMatch"

	// YumUpdateSecurityMatchStr is a String representation of YumUpdateSecurityMatch
	YumUpdateSecurityMatchStr = "YumUpdateSecurityMatch"

	// PkgAuditMatchStr is a String representation of PkgAuditMatch
	PkgAuditMatchStr = "PkgAuditMatch"

	// OvalMatchStr is a String representation of OvalMatch
	OvalMatchStr = "OvalMatch"

	// ChangelogExactMatchStr is a String representation of ChangelogExactMatch
	ChangelogExactMatchStr = "ChangelogExactMatch"

	// ChangelogLenientMatchStr is a String representation of ChangelogLenientMatch
	ChangelogLenientMatchStr = "ChangelogLenientMatch"

	// FailedToGetChangelog is a String representation of FailedToGetChangelog
	FailedToGetChangelog = "FailedToGetChangelog"

	// FailedToFindVersionInChangelog is a String representation of FailedToFindVersionInChangelog
	FailedToFindVersionInChangelog = "FailedToFindVersionInChangelog"
)

// CpeNameMatch is a ranking how confident the CVE-ID was deteted correctly
var CpeNameMatch = Confidence{100, CpeNameMatchStr}

// YumUpdateSecurityMatch is a ranking how confident the CVE-ID was deteted correctly
var YumUpdateSecurityMatch = Confidence{100, YumUpdateSecurityMatchStr}

// PkgAuditMatch is a ranking how confident the CVE-ID was deteted correctly
var PkgAuditMatch = Confidence{100, PkgAuditMatchStr}

// OvalMatch is a ranking how confident the CVE-ID was deteted correctly
var OvalMatch = Confidence{100, OvalMatchStr}

// ChangelogExactMatch is a ranking how confident the CVE-ID was deteted correctly
var ChangelogExactMatch = Confidence{95, ChangelogExactMatchStr}

// ChangelogLenientMatch is a ranking how confident the CVE-ID was deteted correctly
var ChangelogLenientMatch = Confidence{50, ChangelogLenientMatchStr}

// VulnInfos is VulnInfo list, getter/setter, sortable methods.
type VulnInfos []VulnInfo

// FindByCveID find by CVEID
// TODO remove
//  func (v *VulnInfos) FindByCveID(cveID string) (VulnInfo, bool) {
//      for _, p := range s {
//          if cveID == p.CveID {
//              return p, true
//          }
//      }
//      return VulnInfo{CveID: cveID}, false
//  }

// Get VulnInfo by cveID
func (v *VulnInfos) Get(cveID string) (VulnInfo, bool) {
	for _, vv := range *v {
		if vv.CveID == cveID {
			return vv, true
		}
	}
	return VulnInfo{}, false
}

// Delete by cveID
func (v *VulnInfos) Delete(cveID string) {
	vInfos := *v
	for i, vv := range vInfos {
		if vv.CveID == cveID {
			*v = append(vInfos[:i], vInfos[i+1:]...)
			break
		}
	}
}

// Insert VulnInfo
func (v *VulnInfos) Insert(vinfo VulnInfo) {
	*v = append(*v, vinfo)
}

// Update VulnInfo
func (v *VulnInfos) Update(vInfo VulnInfo) (ok bool) {
	for i, vv := range *v {
		if vv.CveID == vInfo.CveID {
			(*v)[i] = vInfo
			return true
		}
	}
	return false
}

// Upsert cveInfo
func (v *VulnInfos) Upsert(vInfo VulnInfo) {
	ok := v.Update(vInfo)
	if !ok {
		v.Insert(vInfo)
	}
}

// immutable
//  func (v *VulnInfos) set(cveID string, v VulnInfo) VulnInfos {
//      for i, p := range s {
//          if cveID == p.CveID {
//              s[i] = v
//              return s
//          }
//      }
//      return append(s, v)
//  }

//TODO GO 1.8
// Len implement Sort Interface
//  func (s VulnInfos) Len() int {
//      return len(s)
//  }

//  // Swap implement Sort Interface
//  func (s VulnInfos) Swap(i, j int) {
//      s[i], s[j] = s[j], s[i]
//  }

//  // Less implement Sort Interface
//  func (s VulnInfos) Less(i, j int) bool {
//      return s[i].CveID < s[j].CveID
//  }

// VulnInfo holds a vulnerability information and unsecure packages
type VulnInfo struct {
	CveID            string
	Confidence       Confidence
	Packages         PackageInfoList
	DistroAdvisories []DistroAdvisory // for Aamazon, RHEL, FreeBSD
	CpeNames         []string
	CveContents      CveContents
}

// NilSliceToEmpty set nil slice fields to empty slice to avoid null in JSON
func (v *VulnInfo) NilSliceToEmpty() {
	if v.CpeNames == nil {
		v.CpeNames = []string{}
	}
	if v.DistroAdvisories == nil {
		v.DistroAdvisories = []DistroAdvisory{}
	}
	if v.Packages == nil {
		v.Packages = PackageInfoList{}
	}
}

// CveInfos is for sorting
//  type CveInfos []CveInfo

//  func (c CveInfos) Len() int {
//      return len(c)
//  }

//  func (c CveInfos) Swap(i, j int) {
//      c[i], c[j] = c[j], c[i]
//  }

//  func (c CveInfos) Less(i, j int) bool {
//      if c[i].CvssV2Score() == c[j].CvssV2Score() {
//          return c[i].CveID < c[j].CveID
//      }
//      return c[j].CvssV2Score() < c[i].CvssV2Score()
//  }

//  // Get cveInfo by cveID
//  func (c CveInfos) Get(cveID string) (CveInfo, bool) {
//      for _, cve := range c {
//          if cve.VulnInfo.CveID == cveID {
//              return cve, true
//          }
//      }
//      return CveInfo{}, false
//  }

//  // Delete by cveID
//  func (c *CveInfos) Delete(cveID string) {
//      cveInfos := *c
//      for i, cve := range cveInfos {
//          if cve.VulnInfo.CveID == cveID {
//              *c = append(cveInfos[:i], cveInfos[i+1:]...)
//              break
//          }
//      }
//  }

//  // Insert cveInfo
//  func (c *CveInfos) Insert(cveInfo CveInfo) {
//      *c = append(*c, cveInfo)
//  }

//  // Update cveInfo
//  func (c CveInfos) Update(cveInfo CveInfo) (ok bool) {
//      for i, cve := range c {
//          if cve.VulnInfo.CveID == cveInfo.VulnInfo.CveID {
//              c[i] = cveInfo
//              return true
//          }
//      }
//      return false
//  }

//  // Upsert cveInfo
//  func (c *CveInfos) Upsert(cveInfo CveInfo) {
//      ok := c.Update(cveInfo)
//      if !ok {
//          c.Insert(cveInfo)
//      }
//  }

//TODO
// CveInfo has CVE detailed Information.
//  type CveInfo struct {
//      VulnInfo
//      CveContents []CveContent
//  }

// Get a CveContent specified by arg
//  func (c *CveInfo) Get(typestr CveContentType) (*CveContent, bool) {
//      for _, cont := range c.CveContents {
//          if cont.Type == typestr {
//              return &cont, true
//          }
//      }
//      return &CveContent{}, false
//  }

//  // Insert a CveContent to specified by arg
//  func (c *CveInfo) Insert(con CveContent) {
//      c.CveContents = append(c.CveContents, con)
//  }

//  // Update a CveContent to specified by arg
//  func (c *CveInfo) Update(to CveContent) bool {
//      for i, cont := range c.CveContents {
//          if cont.Type == to.Type {
//              c.CveContents[i] = to
//              return true
//          }
//      }
//      return false
//  }

//  // CvssV2Score returns CVSS V2 Score
//  func (c *CveInfo) CvssV2Score() float64 {
//      //TODO
//      if cont, found := c.Get(NVD); found {
//          return cont.Cvss2Score
//      } else if cont, found := c.Get(JVN); found {
//          return cont.Cvss2Score
//      } else if cont, found := c.Get(RedHat); found {
//          return cont.Cvss2Score
//      }
//      return -1
//  }

//  // NilSliceToEmpty set nil slice fields to empty slice to avoid null in JSON
//  func (c *CveInfo) NilSliceToEmpty() {
//      return
//      // TODO
//      //  if c.CveDetail.Nvd.Cpes == nil {
//      //      c.CveDetail.Nvd.Cpes = []cve.Cpe{}
//      //  }
//      //  if c.CveDetail.Jvn.Cpes == nil {
//      //      c.CveDetail.Jvn.Cpes = []cve.Cpe{}
//      //  }
//      //  if c.CveDetail.Nvd.References == nil {
//      //      c.CveDetail.Nvd.References = []cve.Reference{}
//      //  }
//      //  if c.CveDetail.Jvn.References == nil {
//      //      c.CveDetail.Jvn.References = []cve.Reference{}
//      //  }
//  }

// CveContentType is a source of CVE information
type CveContentType string

const (
	// NVD is NVD
	NVD CveContentType = "nvd"

	// JVN is JVN
	JVN CveContentType = "jvn"

	// RedHat is RedHat
	RedHat CveContentType = "redhat"

	// CentOS is CentOS
	CentOS CveContentType = "centos"

	// Debian is Debian
	Debian CveContentType = "debian"

	// Ubuntu is Ubuntu
	Ubuntu CveContentType = "ubuntu"
)

// CveContents has slice of CveContent
type CveContents []CveContent

// Get CveContent by cveID
// TODO Pointer
func (v *CveContents) Get(typestr CveContentType) (CveContent, bool) {
	for _, vv := range *v {
		if vv.Type == typestr {
			return vv, true
		}
	}
	return CveContent{}, false
}

// Delete by cveID
func (v *CveContents) Delete(typestr CveContentType) {
	cveContents := *v
	for i, cc := range cveContents {
		if cc.Type == typestr {
			*v = append(cveContents[:i], cveContents[i+1:]...)
			break
		}
	}
}

// Insert CveContent
func (v *CveContents) Insert(cont CveContent) {
	*v = append(*v, cont)
}

// Update VulnInfo
func (v *CveContents) Update(cont CveContent) (ok bool) {
	for i, vv := range *v {
		if vv.Type == cont.Type {
			(*v)[i] = cont
			return true
		}
	}
	return false
}

// Upsert CveContent
func (v *CveContents) Upsert(cont CveContent) {
	ok := v.Update(cont)
	if !ok {
		v.Insert(cont)
	}
}

// CvssV2Score returns CVSS V2 Score
func (v *CveContents) CvssV2Score() float64 {
	//TODO
	if cont, found := v.Get(NVD); found {
		return cont.Cvss2Score
	} else if cont, found := v.Get(JVN); found {
		return cont.Cvss2Score
	} else if cont, found := v.Get(RedHat); found {
		return cont.Cvss2Score
	}
	return -1
}

// CveContent has abstraction of various vulnerability information
type CveContent struct {
	Type         CveContentType
	CveID        string
	Title        string
	Summary      string
	Severity     string
	Cvss2Score   float64
	Cvss2Vector  string
	Cvss3Score   float64
	Cvss3Vector  string
	Cpes         []Cpe
	References   []Reference
	CweID        string
	Published    time.Time
	LastModified time.Time
}

// Empty checks the content is empty
func (c CveContent) Empty() bool {
	return c.Summary == ""
}

// Cpe is Common Platform Enumeration
type Cpe struct {
	CpeName string
}

// Reference has a related link of the CVE
type Reference struct {
	RefID  string
	Source string
	Link   string
}

// PackageInfoList is slice of PackageInfo
type PackageInfoList []PackageInfo

// Exists returns true if exists the name
func (ps PackageInfoList) Exists(name string) bool {
	for _, p := range ps {
		if p.Name == name {
			return true
		}
	}
	return false
}

// UniqByName be uniq by name.
func (ps PackageInfoList) UniqByName() (distincted PackageInfoList) {
	set := make(map[string]PackageInfo)
	for _, p := range ps {
		set[p.Name] = p
	}
	//sort by key
	keys := []string{}
	for key := range set {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		distincted = append(distincted, set[key])
	}
	return
}

// FindByName search PackageInfo by name
func (ps PackageInfoList) FindByName(name string) (result PackageInfo, found bool) {
	for _, p := range ps {
		if p.Name == name {
			return p, true
		}
	}
	return PackageInfo{}, false
}

// MergeNewVersion merges candidate version information to the receiver struct
func (ps PackageInfoList) MergeNewVersion(as PackageInfoList) {
	for _, a := range as {
		for i, p := range ps {
			if p.Name == a.Name {
				ps[i].NewVersion = a.NewVersion
				ps[i].NewRelease = a.NewRelease
			}
		}
	}
}

func (ps PackageInfoList) countUpdatablePacks() int {
	count := 0
	set := make(map[string]bool)
	for _, p := range ps {
		if len(p.NewVersion) != 0 && !set[p.Name] {
			count++
			set[p.Name] = true
		}
	}
	return count
}

// FormatUpdatablePacksSummary returns a summary of updatable packages
func (ps PackageInfoList) FormatUpdatablePacksSummary() string {
	return fmt.Sprintf("%d updatable packages",
		ps.countUpdatablePacks())
}

// Find search PackageInfo by name-version-release
//  func (ps PackageInfoList) find(nameVersionRelease string) (PackageInfo, bool) {
//      for _, p := range ps {
//          joined := p.Name
//          if 0 < len(p.Version) {
//              joined = fmt.Sprintf("%s-%s", joined, p.Version)
//          }
//          if 0 < len(p.Release) {
//              joined = fmt.Sprintf("%s-%s", joined, p.Release)
//          }
//          if joined == nameVersionRelease {
//              return p, true
//          }
//      }
//      return PackageInfo{}, false
//  }

// PackageInfosByName implements sort.Interface for []PackageInfo based on
// the Name field.
type PackageInfosByName []PackageInfo

func (a PackageInfosByName) Len() int           { return len(a) }
func (a PackageInfosByName) Swap(i, j int)      { a[i], a[j] = a[j], a[i] }
func (a PackageInfosByName) Less(i, j int) bool { return a[i].Name < a[j].Name }

// PackageInfo has installed packages.
type PackageInfo struct {
	Name        string
	Version     string
	Release     string
	NewVersion  string
	NewRelease  string
	Repository  string
	Changelog   Changelog
	NotFixedYet bool // Ubuntu OVAL Only
}

// Changelog has contents of changelog and how to get it.
// Method: modesl.detectionMethodStr
type Changelog struct {
	Contents string
	Method   string
}

// FormatCurrentVer returns package name-version-release
func (p PackageInfo) FormatCurrentVer() string {
	str := p.Name
	if 0 < len(p.Version) {
		str = fmt.Sprintf("%s-%s", str, p.Version)
	}
	if 0 < len(p.Release) {
		str = fmt.Sprintf("%s-%s", str, p.Release)
	}
	return str
}

// FormatNewVer returns package name-version-release
func (p PackageInfo) FormatNewVer() string {
	str := p.Name
	if 0 < len(p.NewVersion) {
		str = fmt.Sprintf("%s-%s", str, p.NewVersion)
	}
	if 0 < len(p.NewRelease) {
		str = fmt.Sprintf("%s-%s", str, p.NewRelease)
	}
	return str
}

// DistroAdvisory has Amazon Linux, RHEL, FreeBSD Security Advisory information.
type DistroAdvisory struct {
	AdvisoryID string
	Severity   string
	Issued     time.Time
	Updated    time.Time
}

// Container has Container information
type Container struct {
	ContainerID string
	Name        string
	Image       string
	Type        string
}

// Platform has platform information
type Platform struct {
	Name       string // aws or azure or gcp or other...
	InstanceID string
}

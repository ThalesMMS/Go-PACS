package main

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"image/color"
	"io"
	"net"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strconv"
	"strings"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/app"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/driver/desktop"
	"fyne.io/fyne/v2/layout"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"

	"github.com/ThalesMMS/Go-PACS/internal/appconfig"
	"github.com/ThalesMMS/Go-PACS/internal/archive"
	"github.com/ThalesMMS/Go-PACS/internal/autoquery"
	"github.com/ThalesMMS/Go-PACS/internal/core"
	"github.com/ThalesMMS/Go-PACS/internal/dicominspect"
	studyexport "github.com/ThalesMMS/Go-PACS/internal/export"
	"github.com/ThalesMMS/Go-PACS/internal/netverify"
	"github.com/ThalesMMS/Go-PACS/internal/nodes"
	ops "github.com/ThalesMMS/Go-PACS/internal/operations"
	"github.com/ThalesMMS/Go-PACS/internal/query"
	"github.com/ThalesMMS/Go-PACS/internal/receive"
	"github.com/ThalesMMS/Go-PACS/internal/retrieve"
	"github.com/ThalesMMS/Go-PACS/internal/send"
)

const maxTaskHistory = ops.MaxHistoryEntries
const defaultWindowWidth float32 = 1600
const defaultWindowHeight float32 = 900
const workstationCompactScale float32 = 0.86

// Workstation density overrides. The Horos reference is a native macOS app with
// ~16-19px logical table rows; Fyne's default padding pushes rows to ~30px+. To
// approach that density we shrink the inner/outer padding well below the scaled
// base values (Fyne base inner padding is 8, padding 4). Native controls
// (checkbox/select) still floor the network table rows.
const workstationInnerPadding float32 = 3
const workstationPadding float32 = 2

var queryModalityCodes = []string{
	"CR", "CT", "MG", "XA", "RF", "NM", "DX", "ES", "PT",
	"SR", "SC", "MR", "AU", "OT", "RG", "DR", "XC", "VL", "US",
}

var autoQueryProfileLockIconResource = theme.NewThemedResource(fyne.NewStaticResource("auto-query-profile-lock.svg", []byte(`<svg viewBox="0 0 24 24" xmlns="http://www.w3.org/2000/svg"><path d="M7 10V8a5 5 0 0 1 10 0v2h1a1 1 0 0 1 1 1v9a1 1 0 0 1-1 1H6a1 1 0 0 1-1-1v-9a1 1 0 0 1 1-1h1zm2 0h6V8a3 3 0 0 0-6 0v2zm2 5.73V18h2v-2.27a2 2 0 1 0-2 0z"/></svg>`)))
var queryRetrieveRowIconResource = fyne.NewStaticResource("query-retrieve-row-download-green.svg", []byte(`<svg viewBox="0 0 24 24" xmlns="http://www.w3.org/2000/svg"><path d="M10 3h4v9h4l-6 7-6-7h4z" fill="#80d95a"/><path d="M5 19h14v2H5z" fill="#5fbf3f"/><path d="M12 19 6 12h4V4h2z" fill="#9bef72" opacity=".9"/></svg>`))
var mainToolbarTransferDownIconResource = fyne.NewStaticResource("main-toolbar-transfer-down-green.svg", []byte(`<svg viewBox="0 0 48 48" xmlns="http://www.w3.org/2000/svg"><path d="M12 5h16l8 8v18h-8v-8h-8v8h-8z" fill="#f3f6f8"/><path d="M28 5v8h8z" fill="#cfd6de"/><path d="M22 15h12v13h8L28 43 14 28h8z" fill="#5cc23d"/><path d="M22 15h12v4H22zm6 28L14 28h8V18h6z" fill="#6eda4b"/><path d="M12 5h16l8 8v10h-3v-8h-8V8H15v20h-3z" fill="#ffffff" opacity=".55"/></svg>`))
var mainToolbarTransferUpIconResource = fyne.NewStaticResource("main-toolbar-transfer-up-blue.svg", []byte(`<svg viewBox="0 0 48 48" xmlns="http://www.w3.org/2000/svg"><path d="M12 15h8v8h8v-8h8v28H12z" fill="#f3f6f8"/><path d="M14 41h20V18h-4v8H18v-8h-4z" fill="#dfe5ea"/><path d="M22 33h12V20h8L28 5 14 20h8z" fill="#4c9bd7"/><path d="M28 5 14 20h8v13h6z" fill="#6bb7ed"/><path d="M12 24h3v16h18v-8h3v11H12z" fill="#ffffff" opacity=".5"/></svg>`))
var mainToolbarAnonymizeIconResource = fyne.NewStaticResource("main-toolbar-anonymize-question.svg", []byte(`<svg viewBox="0 0 48 48" xmlns="http://www.w3.org/2000/svg"><path d="M12 5h18l6 6v27H12z" fill="#f3f6f8"/><path d="M30 5v7h7z" fill="#cfd6de"/><path d="M22 31h5v5h-5zM18 17c.3-5 4.1-8 9.3-8 5.1 0 8.7 3 8.7 7.4 0 3.1-1.6 5-4.7 6.8-2.7 1.6-3.7 2.8-3.8 5.2h-5.1c0-4 1.6-6.1 4.8-8 2.3-1.4 3.2-2.4 3.2-4.2 0-2-1.5-3.3-3.8-3.3-2.4 0-4 1.4-4.3 3.9z" fill="#1c2025"/><path d="M11 5h19l7 7v7h-4v-5h-7V8H14v30h-3z" fill="#ffffff" opacity=".5"/></svg>`))
var mainToolbarMetadataIconResource = fyne.NewStaticResource("main-toolbar-metadata-warning.svg", []byte(`<svg viewBox="0 0 48 48" xmlns="http://www.w3.org/2000/svg"><path d="M8 9h32v27H8z" fill="#22272e"/><path d="M11 12h26v21H11z" fill="#11151a"/><path d="M14 15h9v3h-9zm0 6h13v3H14zm0 6h8v3h-8z" fill="#f3c13a"/><path d="M29 14 42 37H16z" fill="#e3b21f"/><path d="M29 20v9" stroke="#1b1b1b" stroke-width="4" stroke-linecap="round"/><circle cx="29" cy="34" r="2.4" fill="#1b1b1b"/><path d="M8 9h32v4H12v23H8z" fill="#ffffff" opacity=".18"/></svg>`))
var mainToolbarDeleteIconResource = fyne.NewStaticResource("main-toolbar-delete-trash.svg", []byte(`<svg viewBox="0 0 48 48" xmlns="http://www.w3.org/2000/svg"><path d="M14 14h20l-2 27H16z" fill="#b8c0c8"/><path d="M12 11h24v5H12z" fill="#d7dde3"/><path d="M19 7h10l2 4H17z" fill="#eef2f5"/><path d="M19 20v16m5-16v16m5-16v16" stroke="#5f6870" stroke-width="3" stroke-linecap="round"/><path d="M16 16h16l-.5 6H16.5z" fill="#89929b" opacity=".45"/><path d="M12 11h24v2H12z" fill="#ffffff" opacity=".65"/></svg>`))
var archiveAlbumDatabaseIconResource = fyne.NewStaticResource("archive-album-database-blue.svg", []byte(`<svg viewBox="0 0 24 24" xmlns="http://www.w3.org/2000/svg"><path d="M3 6h7l2 2h9v11H3z" fill="#6ca7e8"/><path d="M4 9h16v9H4z" fill="#4f8fd2"/><path d="M6 11h12v5H6z" fill="#2f5f93" opacity=".55"/><path d="M3 6h7l2 2h9v2H3z" fill="#9cc8f2"/><path d="M5 12h4v1.8H5zm0 3h8v1.8H5z" fill="#eaf4ff" opacity=".75"/></svg>`))
var archiveAlbumCommentsIconResource = fyne.NewStaticResource("archive-album-comments-purple.svg", []byte(`<svg viewBox="0 0 24 24" xmlns="http://www.w3.org/2000/svg"><path d="M3 6h7l2 2h9v11H3z" fill="#a77be8"/><path d="M4 9h16v9H4z" fill="#805cc5"/><path d="M6 11h12v5H6z" fill="#4d3a79" opacity=".55"/><path d="M8 12h7v1.5H8zm0 3h5v1.5H8z" fill="#f3eaff"/><path d="M6 10h4l1 2H6z" fill="#d7c0ff"/></svg>`))
var archiveAlbumInterestingIconResource = fyne.NewStaticResource("archive-album-interesting-blue.svg", []byte(`<svg viewBox="0 0 24 24" xmlns="http://www.w3.org/2000/svg"><path d="M3 6h7l2 2h9v11H3z" fill="#70aeea"/><path d="M4 9h16v9H4z" fill="#397cbf"/><path d="M12 11.2l1.2 2.4 2.7.4-2 1.9.5 2.7-2.4-1.3-2.4 1.3.5-2.7-2-1.9 2.7-.4z" fill="#ffd34f"/><path d="M3 6h7l2 2h9v2H3z" fill="#a7d3f5"/></svg>`))
var archiveAlbumAcquiredClockIconResource = fyne.NewStaticResource("archive-album-acquired-clock-purple.svg", []byte(`<svg viewBox="0 0 24 24" xmlns="http://www.w3.org/2000/svg"><path d="M3 6h7l2 2h9v11H3z" fill="#a77be8"/><path d="M4 9h16v9H4z" fill="#7651bf"/><path d="M3 6h7l2 2h9v2H3z" fill="#d4bdff"/><circle cx="12.2" cy="14.2" r="4.2" fill="#efe7ff"/><path d="M12.2 11.6v2.9l2.3 1.3" fill="none" stroke="#5d3c9e" stroke-width="1.7" stroke-linecap="round" stroke-linejoin="round"/><path d="M6 11h3.5v1.5H6z" fill="#f6f0ff" opacity=".8"/></svg>`))
var archiveAlbumAddedClockIconResource = fyne.NewStaticResource("archive-album-added-clock-purple.svg", []byte(`<svg viewBox="0 0 24 24" xmlns="http://www.w3.org/2000/svg"><path d="M3 6h7l2 2h9v11H3z" fill="#a77be8"/><path d="M4 9h16v9H4z" fill="#7651bf"/><path d="M3 6h7l2 2h9v2H3z" fill="#d4bdff"/><circle cx="12.2" cy="14.2" r="4.2" fill="#efe7ff"/><path d="M12.2 11.7v5m-2.5-2.5h5" stroke="#5d3c9e" stroke-width="1.7" stroke-linecap="round"/><path d="M6 11h3.4v1.5H6z" fill="#f6f0ff" opacity=".8"/></svg>`))
var archiveAlbumTodayCRIconResource = fyne.NewStaticResource("archive-album-today-cr-purple.svg", []byte(`<svg viewBox="0 0 24 24" xmlns="http://www.w3.org/2000/svg"><path d="M3 6h7l2 2h9v11H3z" fill="#a77be8"/><path d="M4 9h16v9H4z" fill="#7651bf"/><path d="M3 6h7l2 2h9v2H3z" fill="#d4bdff"/><path d="M7 12h10v5H7z" fill="#efe7ff"/><path d="M7 12h10v1.6H7z" fill="#cdb4ff"/><path d="M9.2 16c-.8 0-1.4-.6-1.4-1.4s.6-1.4 1.4-1.4c.4 0 .8.2 1.1.5l-.7.7c-.1-.1-.2-.2-.4-.2-.3 0-.4.2-.4.4s.2.4.4.4c.2 0 .3-.1.4-.2l.7.7c-.3.3-.7.5-1.1.5zm2.2 0v-2.8h1.5c.7 0 1.1.4 1.1 1 0 .4-.2.7-.5.8l.7 1h-1.1l-.5-.8h-.2v.8zm1-1.6h.4c.2 0 .3-.1.3-.2s-.1-.2-.3-.2h-.4z" fill="#5d3c9e"/></svg>`))
var archiveAlbumTodayCTIconResource = fyne.NewStaticResource("archive-album-today-ct-purple.svg", []byte(`<svg viewBox="0 0 24 24" xmlns="http://www.w3.org/2000/svg"><path d="M3 6h7l2 2h9v11H3z" fill="#a77be8"/><path d="M4 9h16v9H4z" fill="#7651bf"/><path d="M3 6h7l2 2h9v2H3z" fill="#d4bdff"/><path d="M7 12h10v5H7z" fill="#efe7ff"/><path d="M7 12h10v1.6H7z" fill="#cdb4ff"/><path d="M9.2 16c-.8 0-1.4-.6-1.4-1.4s.6-1.4 1.4-1.4c.4 0 .8.2 1.1.5l-.7.7c-.1-.1-.2-.2-.4-.2-.3 0-.4.2-.4.4s.2.4.4.4c.2 0 .3-.1.4-.2l.7.7c-.3.3-.7.5-1.1.5zm2.5 0v-1.8h-.8v-1h2.6v1h-.8V16z" fill="#5d3c9e"/></svg>`))
var archiveSourceLocalDBIconResource = fyne.NewStaticResource("archive-source-local-db-blue.svg", []byte(`<svg viewBox="0 0 24 24" xmlns="http://www.w3.org/2000/svg"><rect x="3" y="5" width="16" height="15" rx="2" fill="#4f8fd2"/><path d="M6 8h10v2H6zm0 4h10v2H6zm0 4h7v2H6z" fill="#eaf4ff"/><path d="M17 7l4 4-4 4z" fill="#7bd6ff"/><path d="M3 5h16v3H3z" fill="#9cc8f2"/></svg>`))
var archiveSourceReceiverIconResource = fyne.NewStaticResource("archive-source-receiver-red.svg", []byte(`<svg viewBox="0 0 24 24" xmlns="http://www.w3.org/2000/svg"><rect x="4" y="7" width="14" height="12" rx="2" fill="#b63b37"/><path d="M6 10h10v2H6zm0 4h7v2H6z" fill="#ffe8e6"/><circle cx="18" cy="8" r="3.2" fill="#f04b45"/><path d="M18 6.4v3.2m-1.6-1.6h3.2" stroke="#fff1ef" stroke-width="1.3" stroke-linecap="round"/><path d="M4 7h14v3H4z" fill="#ee756e"/></svg>`))
var archiveSourceRemoteNodeIconResource = fyne.NewStaticResource("archive-source-remote-globe-blue.svg", []byte(`<svg viewBox="0 0 24 24" xmlns="http://www.w3.org/2000/svg"><circle cx="12" cy="12" r="8.5" fill="#69a7df"/><path d="M4.8 12h14.4M12 3.8c2 2.2 3 5 3 8.2s-1 6-3 8.2c-2-2.2-3-5-3-8.2s1-6 3-8.2z" fill="none" stroke="#dff2ff" stroke-width="1.4"/><path d="M6.4 7.8h11.2M6.4 16.2h11.2" stroke="#2f6fa9" stroke-width="1.4" stroke-linecap="round"/><path d="M5 9c1-3 3.7-5 7-5 2.2 0 4.2.8 5.7 2.2-6 .2-9.9 1.1-12.7 2.8z" fill="#a7d7f6" opacity=".8"/></svg>`))

func autoQueryProfileLockIcon() fyne.Resource {
	return autoQueryProfileLockIconResource
}

const (
	queryDatePresetAny                = "Any date"
	queryDatePresetOn                 = "On:"
	queryDatePresetBetween            = "Between:"
	queryDatePresetTodayAM            = "Today AM"
	queryDatePresetTodayPM            = "Today PM"
	queryDatePresetToday              = "Today"
	queryDatePresetYesterday          = "Yesterday"
	queryDatePresetDayBeforeYesterday = "Day Before Yesterday"
	queryDatePresetLast2Days          = "Last 2 days"
	queryDatePresetLast7Days          = "Last 7 days"
	queryDatePresetLastMonth          = "Last month"
	queryDatePresetLast3Months        = "Last 3 months"
	queryDatePresetLast30Min          = "Last 30 min"
	queryDatePresetLast1Hour          = "Last 1 hour"
	queryDatePresetLast2Hours         = "Last 2 hours"
	queryDatePresetLast3Hours         = "Last 3 hours"
	queryDatePresetLast6Hours         = "Last 6 hours"
	queryDatePresetLast8Hours         = "Last 8 hours"
	queryDatePresetLast12Hours        = "Last 12 hours"
	queryDatePresetLast24Hours        = "Last 24 hours"
	queryDatePresetLastNHours         = "Last N hours"
)

const (
	queryQuickSearchPatientName        = "Name"
	queryQuickSearchPatientID          = "Patient ID"
	queryQuickSearchAccession          = "Accession Number"
	queryQuickSearchBirthdate          = "Birthdate"
	queryQuickSearchDescription        = "Description"
	queryQuickSearchReferringPhysician = "Referring Physician"
	queryQuickSearchInstitution        = "Institution"
	queryQuickSearchComments           = "Comments"
	queryQuickSearchCustomDICOMField   = "Custom DICOM field"
	queryQuickSearchStatus             = "Status"
)

const queryQuickSearchSegmentMinWidth float32 = 72
const queryQuickSearchSegmentHeight float32 = 24
const queryQuickSearchSelectedSegmentHorizontalInset float32 = 3
const queryQuickSearchSelectedSegmentVerticalInset float32 = 2

const (
	queryWorkspaceTitle      = "DICOM Query/Retrieve"
	queryActionLabelQuery    = "Query"
	queryActionLabelPatient  = "Query Patient"
	queryActionLabelSeries   = "Series"
	queryActionLabelImages   = "Images"
	queryActionLabelRetrieve = "Retrieve"
	queryActionLabelVerify   = "Verify"
	dicomNodesTitle          = "DICOM Nodes:"
	dicomNodesInstruction    = "Drag sources into the priority order for retrieving"
)

const queryPrimaryActionButtonMinWidth float32 = 92
const autoQueryProfileIconSlotSize float32 = 36
const autoQueryProfileSelectSlotWidth float32 = 420

const (
	queryDateFilterPanelMinWidth       float32 = 560
	queryModalityFilterPanelMinWidth   float32 = 152
	queryModalityCheckSlotWidth        float32 = 64
	querySearchBarEntryWidth           float32 = 820
	queryRetrieveDestinationSlotWidth  float32 = 520
	queryRefreshCadenceSlotWidth       float32 = 220
	queryRefreshCountdownSlotWidth     float32 = 96
	autoQueryRefreshCountdownSlotWidth float32 = 72
	queryAutoRetrieveSlotWidth         float32 = 148
	queryAutoRetrieveSettingsSlotWidth float32 = 96
	autoQueryRetrieveSettingsSlotWidth float32 = 96
)

const (
	toolbarLabelOpen           = "Open"
	toolbarLabelInspect        = "Inspect"
	toolbarLabelImport         = "Import"
	toolbarLabelExport         = "Export"
	toolbarLabelFolder         = "Folder"
	toolbarLabelRefresh        = "Refresh"
	toolbarLabelQuery          = "Query"
	toolbarLabelSendStudy      = "Send"
	toolbarLabelSendSeries     = "Send Series"
	toolbarLabelSendImage      = "Send Image"
	toolbarLabelRetrieveSeries = "Get Series"
	toolbarLabelRetrieveImage  = "Get Image"
	toolbarLabelCancel         = "Cancel"
	toolbarLabelAnonymize      = "Anonymize"
	toolbarLabelMetaData       = "Meta-Data"
	toolbarLabelAdd            = "Add"
	toolbarLabelEdit           = "Edit"
	toolbarLabelDelete         = "Delete"
	toolbarLabelVerify         = "Verify"
	toolbarLabelListen         = "Listen"
	toolbarLabelStop           = "Stop"
	toolbarLabelSettings       = "Settings"
)

const networkTabTitle = "Network"

const (
	networkActionLabelAll        = "All"
	networkActionLabelNone       = "None"
	networkActionLabelSave       = "Save..."
	networkActionLabelLoad       = "Load..."
	networkActionLabelVerify     = "Verify"
	networkActionLabelAddNewNode = "Add new node"
	networkActionLabelEdit       = "Edit"
	networkActionLabelDelete     = "Delete"
)

const (
	queryRefreshModeDont    = "Don't refresh"
	queryRefreshButtonLabel = "Refresh"
	queryAutoRetrieveLabel  = "Auto-Retrieve"
	queryKeepOnTopLabel     = "Keep this window on top of all other windows"
)

const (
	autoQueryTabTitle           = "Auto Q/R"
	autoQueryProfileDefault     = "Default Instance"
	autoQueryRefreshEvery30Min  = "Refresh every 30 min"
	autoQueryCountdownDormant   = "--:--"
	queryCountdownDormant       = ""
	autoQuerySettingsButtonText = "Settings"
)

const (
	autoQueryRetrieveLevelStudy           = "Study"
	autoQueryRetrieveLevelSeries          = "Series"
	autoQueryRetrieveLevelImage           = "Image"
	autoQueryDefaultMaxMatches            = "25"
	autoQueryDuplicatePolicySkipExisting  = "Skip existing"
	autoQueryDuplicatePolicyKeepDuplicate = "Keep duplicate"
	autoQueryDuplicatePolicyReject        = "Reject duplicates"
)

const (
	settingsLabelAETitle                   = "AETitle:"
	settingsLabelReceiverPort              = "Port Number:"
	settingsLabelAddressSummary            = "Address(es):"
	settingsLabelHostName                  = "Host Name:"
	settingsLabelPreferredSyntax           = "Preferred Syntax:"
	settingsLabelDICOMCommunicationTimeout = "Time-out for DICOM communications:"
	settingsLabelDICOMConnectionTimeout    = "Connection time-out:"
	listenerAdvancedBindingTitle           = "Advanced Binding"
	listenerAdvancedSafetyLimitsTitle      = "Advanced Safety Limits"
	receivePreferredSyntaxAutoLabel        = "Auto"
	receivePreferredSyntaxExplicitLabel    = "Explicit VR Little Endian"
	receivePreferredSyntaxImplicitLabel    = "Implicit VR Little Endian"
	sendSyntaxAutoLabel                    = "Auto"
	sendSyntaxExplicitLabel                = "Explicit VR Little Endian"
	sendSyntaxImplicitLabel                = "Implicit VR Little Endian"
	studyStatusPresetCustomLabel           = "Custom"
	studyStatusPresetReviewedLabel         = "Reviewed"
	studyStatusPresetInterestingLabel      = "Interesting"
	studyStatusPresetFollowUpLabel         = "Follow-up"
	studyStatusPresetTeachingLabel         = "Teaching"
	studyStatusPresetProblemLabel          = "Problem"
)

const listenerSettingsActionButtonSlotWidth float32 = 62
const listenerSettingsDialogWidth float32 = 960
const listenerSettingsDialogHeight float32 = 760
const listenerPortEntrySlotWidth float32 = listenerPrimaryEntrySlotWidth
const listenerTimeoutEntrySlotWidth float32 = 78
const listenerIncomingScanEntrySlotWidth float32 = 62
const listenerPreferredSyntaxSlotWidth float32 = 340
const listenerTLSSettingsButtonSlotWidth float32 = 180
const listenerPrimaryEntrySlotWidth float32 = 540
const listenerAddressEntrySlotWidth float32 = 806
const listenerIncomingUnreadableIndentWidth float32 = 304

const (
	listenerIncomingDontModifyLabel       = "Don't modify"
	listenerIncomingDecompressPolicyLabel = "Decompress compressed images"
)

func listenerSettingsDialogSize() fyne.Size {
	return fyne.NewSize(listenerSettingsDialogWidth, listenerSettingsDialogHeight)
}

type queryRunKind string

const (
	queryRunStudy   queryRunKind = "study"
	queryRunPatient queryRunKind = "patient"
	queryRunSeries  queryRunKind = "series"
	queryRunImage   queryRunKind = "image"
)

type lastQueryRequest struct {
	kind    queryRunKind
	study   query.Criteria
	patient query.PatientCriteria
	series  query.SeriesCriteria
	image   query.ImageCriteria
}

type nodeVerifyStatus string

const (
	nodeVerifyOK   nodeVerifyStatus = "ok"
	nodeVerifyFail nodeVerifyStatus = "fail"
)

type querySourceStatus string

const (
	querySourceOK   querySourceStatus = "ok"
	querySourceFail querySourceStatus = "fail"
)

type sourceStatusHistoryKind string

const (
	sourceStatusHistoryKindVerify sourceStatusHistoryKind = "C-ECHO"
	sourceStatusHistoryKindQuery  sourceStatusHistoryKind = "Query"
	sourceStatusHistoryOK         string                  = "OK"
	sourceStatusHistoryFail       string                  = "failed"
	sourceStatusHistoryLimit                              = 6
)

const queryLocalStatePresent = "local"

const (
	queryLocalStateRetrieved      = "retrieved"
	queryLocalStateRetrieveFailed = "retrieve_failed"
)

var (
	sourceStatusIdleColor = color.NRGBA{R: 92, G: 92, B: 92, A: 255}
	sourceStatusOKColor   = color.NRGBA{R: 66, G: 166, B: 82, A: 255}
	sourceStatusFailColor = color.NRGBA{R: 206, G: 76, B: 70, A: 255}

	queryStatusOKColor          = color.NRGBA{R: 66, G: 166, B: 82, A: 255}
	queryStatusPendingColor     = color.NRGBA{R: 211, G: 166, B: 64, A: 255}
	queryStatusFailColor        = color.NRGBA{R: 206, G: 76, B: 70, A: 255}
	queryLocalStatePresentColor = color.NRGBA{R: 66, G: 166, B: 82, A: 255}

	studyStatusReviewedColor    = color.NRGBA{R: 66, G: 166, B: 82, A: 255}
	studyStatusInterestingColor = color.NRGBA{R: 211, G: 166, B: 64, A: 255}
	studyStatusFollowUpColor    = color.NRGBA{R: 86, G: 148, B: 214, A: 255}
	studyStatusTeachingColor    = color.NRGBA{R: 144, G: 116, B: 206, A: 255}
	studyStatusProblemColor     = color.NRGBA{R: 206, G: 76, B: 70, A: 255}
	studyStatusCustomColor      = color.NRGBA{R: 140, G: 140, B: 140, A: 255}

	studyStatusReviewedChipColor    = color.NRGBA{R: 30, G: 58, B: 34, A: 255}
	studyStatusInterestingChipColor = color.NRGBA{R: 60, G: 49, B: 28, A: 255}
	studyStatusFollowUpChipColor    = color.NRGBA{R: 32, G: 48, B: 68, A: 255}
	studyStatusTeachingChipColor    = color.NRGBA{R: 45, G: 38, B: 66, A: 255}
	studyStatusProblemChipColor     = color.NRGBA{R: 62, G: 32, B: 31, A: 255}
	studyStatusCustomChipColor      = color.NRGBA{R: 44, G: 44, B: 44, A: 255}
)

const (
	archiveQuickSearchPatientName                       = "Patient Name"
	archiveQuickSearchPatientID                         = "Patient ID"
	archiveQuickSearchAccession                         = "Accession"
	archiveToolbarQuickSearchFieldSelectorWidth float32 = 130
	archiveToolbarQuickSearchWidth              float32 = 420
)

var queryDatePresetOptions = flattenQueryDatePresetColumns(queryDatePresetColumns())

var queryQuickSearchOptions = []string{
	queryQuickSearchPatientName,
	queryQuickSearchPatientID,
	queryQuickSearchAccession,
	queryQuickSearchBirthdate,
	queryQuickSearchDescription,
	queryQuickSearchReferringPhysician,
	queryQuickSearchComments,
	queryQuickSearchInstitution,
	queryQuickSearchCustomDICOMField,
	queryQuickSearchStatus,
}

var queryRefreshModeOptions = []string{
	queryRefreshModeDont,
	autoQueryRefreshEvery30Min,
}

var autoQueryRefreshModeOptions = []string{
	queryRefreshModeDont,
	autoQueryRefreshEvery30Min,
}

var autoQueryRetrieveLevelOptions = []string{
	autoQueryRetrieveLevelStudy,
	autoQueryRetrieveLevelSeries,
	autoQueryRetrieveLevelImage,
}

var autoQueryDuplicatePolicyOptions = []string{
	autoQueryDuplicatePolicySkipExisting,
	autoQueryDuplicatePolicyKeepDuplicate,
	autoQueryDuplicatePolicyReject,
}

func newQueryAutoRetrieveCheck(state *uiState) *widget.Check {
	check := widget.NewCheck(queryAutoRetrieveLabel, func(enabled bool) {
		if state != nil {
			state.queryAutoRetrieve = enabled
		}
	})
	if state != nil && state.queryAutoRetrieve {
		check.SetChecked(true)
	}
	return check
}

func newQueryKeepOnTopCheck(state *uiState) *widget.Check {
	check := widget.NewCheck(queryKeepOnTopLabel, func(enabled bool) {
		if state != nil {
			state.queryKeepOnTop = enabled
		}
	})
	if state != nil && state.queryKeepOnTop {
		check.SetChecked(true)
	}
	return check
}

func newAutoQueryAutoRetrieveCheck(state *uiState) *widget.Check {
	check := widget.NewCheck(queryAutoRetrieveLabel, func(enabled bool) {
		if state != nil {
			state.autoQueryAutoRetrieve = enabled
		}
	})
	if state != nil && state.autoQueryAutoRetrieve {
		check.SetChecked(true)
	}
	return check
}

type autoQuerySettings struct {
	AutoRetrieve        bool
	AutoRetrieveSet     bool
	RetrieveLevel       string
	MaxMatches          string
	DuplicatePolicy     string
	RequireConfirmation bool
}

type autoQueryAutoRetrievePlan struct {
	Enabled              bool
	RequiresConfirmation bool
	Candidates           []query.Match
	SkippedLocal         int
	RejectedLocal        int
	Limited              bool
	Message              string
	Err                  error
}

func autoQuerySettingsForState(state *uiState) autoQuerySettings {
	settings := autoQuerySettings{
		AutoRetrieve:        true,
		AutoRetrieveSet:     true,
		RetrieveLevel:       autoQueryRetrieveLevelStudy,
		MaxMatches:          autoQueryDefaultMaxMatches,
		DuplicatePolicy:     autoQueryDuplicatePolicySkipExisting,
		RequireConfirmation: true,
	}
	if state == nil {
		return settings
	}
	settings.AutoRetrieve = state.autoQueryAutoRetrieve
	settings.AutoRetrieveSet = true
	if stringInList(state.autoQueryRetrieveLevel, autoQueryRetrieveLevelOptions) {
		settings.RetrieveLevel = state.autoQueryRetrieveLevel
	}
	if value := strings.TrimSpace(state.autoQueryMaxMatches); value != "" {
		settings.MaxMatches = value
	}
	if stringInList(state.autoQueryDuplicatePolicy, autoQueryDuplicatePolicyOptions) {
		settings.DuplicatePolicy = state.autoQueryDuplicatePolicy
	}
	if state.autoQuerySettingsConfigured {
		settings.RequireConfirmation = state.autoQueryRequireConfirmation
	}
	return settings
}

func planAutoQueryAutoRetrieve(state *uiState, matches []query.Match) autoQueryAutoRetrievePlan {
	if state == nil || !state.autoQueryAutoRetrieve {
		return autoQueryAutoRetrievePlan{Message: "Auto Q/R auto-retrieve is off"}
	}
	settings := autoQuerySettingsForState(state)
	plan := autoQueryAutoRetrievePlan{
		Enabled:              true,
		RequiresConfirmation: settings.RequireConfirmation,
		Message:              "No Auto Q/R retrieve candidates",
	}
	maxMatches, err := parseOptionalMaxResults(settings.MaxMatches)
	if err != nil {
		plan.Err = err
		plan.Message = "Auto Q/R auto-retrieve max matches is invalid"
		return plan
	}
	level := autoQueryRetrieveDICOMLevel(settings.RetrieveLevel)
	seen := map[string]bool{}
	for _, match := range matches {
		if queryLocalStateAvailable(match.LocalState) {
			if settings.DuplicatePolicy == autoQueryDuplicatePolicyReject {
				plan.RejectedLocal++
				continue
			}
			if settings.DuplicatePolicy == autoQueryDuplicatePolicySkipExisting {
				plan.SkippedLocal++
				continue
			}
		}
		candidate, ok := autoQueryRetrieveCandidate(match, level)
		if !ok {
			continue
		}
		key := autoQueryRetrieveCandidateKey(candidate)
		if key == "" || seen[key] {
			continue
		}
		seen[key] = true
		if maxMatches == 0 {
			plan.Limited = true
			continue
		}
		if len(plan.Candidates) >= maxMatches {
			plan.Limited = true
			continue
		}
		plan.Candidates = append(plan.Candidates, candidate)
	}
	if plan.RejectedLocal > 0 {
		plan.Candidates = nil
		plan.Err = fmt.Errorf("Auto Q/R auto-retrieve rejected %d local duplicate matches", plan.RejectedLocal)
		plan.Message = "Auto Q/R auto-retrieve stopped by duplicate policy"
		return plan
	}
	if len(plan.Candidates) > 0 {
		plan.Message = fmt.Sprintf("Auto Q/R auto-retrieve planned %d %s retrieves", len(plan.Candidates), strings.ToLower(level))
	}
	return plan
}

func autoQueryMatchesHandler(w fyne.Window, status *widget.Label, tables archiveTables, state *uiState) queryMatchesHandler {
	return func() {
		var matches []query.Match
		if state != nil {
			matches = state.queries
		}
		plan := planAutoQueryAutoRetrieve(state, matches)
		if !plan.Enabled {
			return
		}
		if plan.Err != nil {
			if status != nil {
				status.SetText(plan.Message)
			}
			if w != nil {
				dialog.ShowError(plan.Err, w)
			}
			return
		}
		if len(plan.Candidates) == 0 {
			if status != nil {
				status.SetText(plan.Message)
			}
			return
		}
		if plan.RequiresConfirmation {
			if status != nil {
				status.SetText(fmt.Sprintf("Auto Q/R auto-retrieve awaiting confirmation for %d matches", len(plan.Candidates)))
			}
			if w == nil {
				return
			}
			dialog.ShowConfirm("Auto Q/R Auto-Retrieve", autoQueryAutoRetrieveConfirmationMessage(plan), func(ok bool) {
				if ok {
					runAutoQueryAutoRetrieve(w, status, tables, state, plan.Candidates)
				}
			}, w)
			return
		}
		runAutoQueryAutoRetrieve(w, status, tables, state, plan.Candidates)
	}
}

func autoQueryAutoRetrieveConfirmationMessage(plan autoQueryAutoRetrievePlan) string {
	parts := []string{fmt.Sprintf("Retrieve %d matching rows automatically?", len(plan.Candidates))}
	if plan.SkippedLocal > 0 {
		parts = append(parts, fmt.Sprintf("%d local duplicates will be skipped.", plan.SkippedLocal))
	}
	if plan.Limited {
		parts = append(parts, "The configured max matches limit was applied.")
	}
	return strings.Join(parts, "\n")
}

func autoQueryRetrieveDICOMLevel(level string) string {
	switch strings.TrimSpace(level) {
	case autoQueryRetrieveLevelSeries:
		return "SERIES"
	case autoQueryRetrieveLevelImage:
		return "IMAGE"
	default:
		return "STUDY"
	}
}

func autoQueryRetrieveCandidate(match query.Match, level string) (query.Match, bool) {
	if !queryUIDAvailable(match.StudyInstanceUID) {
		return query.Match{}, false
	}
	match.QueryRetrieveLevel = level
	switch level {
	case "IMAGE":
		if !queryUIDAvailable(match.SeriesInstanceUID) || !queryUIDAvailable(match.SOPInstanceUID) {
			return query.Match{}, false
		}
	case "SERIES":
		if !queryUIDAvailable(match.SeriesInstanceUID) {
			return query.Match{}, false
		}
		match.SOPInstanceUID = ""
	default:
		match.SeriesInstanceUID = ""
		match.SOPInstanceUID = ""
	}
	return match, true
}

func autoQueryRetrieveCandidateKey(match query.Match) string {
	switch strings.ToUpper(strings.TrimSpace(match.QueryRetrieveLevel)) {
	case "IMAGE":
		return strings.Join([]string{"IMAGE", strings.TrimSpace(match.StudyInstanceUID), strings.TrimSpace(match.SeriesInstanceUID), strings.TrimSpace(match.SOPInstanceUID)}, "\x00")
	case "SERIES":
		return strings.Join([]string{"SERIES", strings.TrimSpace(match.StudyInstanceUID), strings.TrimSpace(match.SeriesInstanceUID)}, "\x00")
	default:
		return strings.Join([]string{"STUDY", strings.TrimSpace(match.StudyInstanceUID)}, "\x00")
	}
}

func applyAutoQuerySettings(state *uiState, settings autoQuerySettings) {
	if state == nil {
		return
	}
	if !stringInList(settings.RetrieveLevel, autoQueryRetrieveLevelOptions) {
		settings.RetrieveLevel = autoQueryRetrieveLevelStudy
	}
	if strings.TrimSpace(settings.MaxMatches) == "" {
		settings.MaxMatches = autoQueryDefaultMaxMatches
	}
	if !stringInList(settings.DuplicatePolicy, autoQueryDuplicatePolicyOptions) {
		settings.DuplicatePolicy = autoQueryDuplicatePolicySkipExisting
	}
	if settings.AutoRetrieveSet {
		state.autoQueryAutoRetrieve = settings.AutoRetrieve
	}
	state.autoQueryRetrieveLevel = settings.RetrieveLevel
	state.autoQueryMaxMatches = strings.TrimSpace(settings.MaxMatches)
	state.autoQueryDuplicatePolicy = settings.DuplicatePolicy
	state.autoQueryRequireConfirmation = settings.RequireConfirmation
	state.autoQuerySettingsConfigured = true
}

func autoQuerySettingsFromProfile(profile autoquery.Profile) autoQuerySettings {
	return autoQuerySettings{
		AutoRetrieve:        profile.Settings.AutoRetrieve,
		AutoRetrieveSet:     true,
		RetrieveLevel:       profile.Settings.RetrieveLevel,
		MaxMatches:          profile.Settings.MaxMatches,
		DuplicatePolicy:     profile.Settings.DuplicatePolicy,
		RequireConfirmation: profile.Settings.RequireConfirmation,
	}
}

func normalizeAutoQueryProfiles(profiles []autoquery.Profile) []autoquery.Profile {
	var defaultProfile autoquery.Profile
	hasDefault := false
	for _, profile := range profiles {
		if strings.EqualFold(strings.TrimSpace(profile.Name), autoquery.DefaultProfileName) {
			defaultProfile = profile
			hasDefault = true
			break
		}
	}
	if !hasDefault {
		defaultProfile = autoquery.DefaultProfile()
	}
	defaultProfile.Name = autoquery.DefaultProfileName
	normalized := []autoquery.Profile{defaultProfile}
	seen := map[string]bool{strings.ToLower(autoquery.DefaultProfileName): true}
	for _, profile := range profiles {
		name := strings.TrimSpace(profile.Name)
		if name == "" {
			continue
		}
		key := strings.ToLower(name)
		if seen[key] {
			continue
		}
		profile.Name = name
		normalized = append(normalized, profile)
		seen[key] = true
	}
	return normalized
}

func autoQueryProfileNames(state *uiState) []string {
	profiles := []autoquery.Profile{autoquery.DefaultProfile()}
	if state != nil && len(state.autoQueryProfiles) > 0 {
		profiles = state.autoQueryProfiles
	}
	profiles = normalizeAutoQueryProfiles(profiles)
	names := make([]string, 0, len(profiles))
	for _, profile := range profiles {
		names = append(names, profile.Name)
	}
	return names
}

func selectedAutoQueryProfileName(state *uiState) string {
	if state != nil && strings.TrimSpace(state.autoQueryProfileName) != "" {
		return strings.TrimSpace(state.autoQueryProfileName)
	}
	return autoquery.DefaultProfileName
}

func autoQueryProfileLocked(state *uiState) bool {
	return state != nil && state.autoQueryProfileLocked
}

func setAutoQueryProfileLocked(state *uiState, locked bool) {
	if state == nil {
		return
	}
	state.autoQueryProfileLocked = locked
}

func selectAutoQueryProfile(state *uiState, name string) bool {
	if state == nil {
		return false
	}
	name = strings.TrimSpace(name)
	if name == "" {
		name = autoquery.DefaultProfileName
	}
	profiles := normalizeAutoQueryProfiles(state.autoQueryProfiles)
	for _, profile := range profiles {
		if strings.EqualFold(strings.TrimSpace(profile.Name), name) {
			state.autoQueryProfiles = profiles
			state.autoQueryProfileName = profile.Name
			state.autoQueryProfileLocked = profile.Locked
			state.autoQueryLast = lastQueryRequest{}
			stopAutoQueryRefresh(state)
			applyAutoQuerySettings(state, autoQuerySettingsFromProfile(profile))
			applyAutoQueryCriteria(state, profile.Criteria)
			applyAutoQuerySources(state, profile.Sources)
			return true
		}
	}
	return false
}

func nextAutoQueryProfileName(profiles []autoquery.Profile) string {
	existing := make(map[string]bool, len(profiles))
	for _, profile := range profiles {
		existing[strings.ToLower(strings.TrimSpace(profile.Name))] = true
	}
	for i := 2; ; i++ {
		name := fmt.Sprintf("Instance %d", i)
		if !existing[strings.ToLower(name)] {
			return name
		}
	}
}

func addAutoQueryProfile(state *uiState) autoquery.Profile {
	if state == nil {
		return autoquery.DefaultProfile()
	}
	profiles := normalizeAutoQueryProfiles(state.autoQueryProfiles)
	state.autoQueryProfiles = profiles
	profile := autoQueryProfileFromState(state)
	profile.Name = nextAutoQueryProfileName(profiles)
	state.autoQueryProfiles = append(profiles, profile)
	state.autoQueryProfileName = profile.Name
	return profile
}

func removeSelectedAutoQueryProfile(state *uiState) bool {
	if state == nil {
		return false
	}
	selected := selectedAutoQueryProfileName(state)
	if strings.EqualFold(selected, autoquery.DefaultProfileName) {
		return false
	}
	profiles := normalizeAutoQueryProfiles(state.autoQueryProfiles)
	filtered := profiles[:0]
	removed := false
	for _, profile := range profiles {
		if strings.EqualFold(strings.TrimSpace(profile.Name), selected) {
			removed = true
			continue
		}
		filtered = append(filtered, profile)
	}
	if !removed {
		return false
	}
	state.autoQueryProfiles = normalizeAutoQueryProfiles(filtered)
	_ = selectAutoQueryProfile(state, autoquery.DefaultProfileName)
	return true
}

func renameSelectedAutoQueryProfile(state *uiState, newName string) error {
	if state == nil {
		return errors.New("Auto Q/R profile state is unavailable")
	}
	current := selectedAutoQueryProfileName(state)
	if strings.EqualFold(current, autoquery.DefaultProfileName) {
		return errors.New("Default Auto Q/R profile cannot be renamed")
	}
	if autoQueryProfileLocked(state) {
		return errors.New("Auto Q/R profile is locked")
	}
	newName = strings.TrimSpace(newName)
	if newName == "" {
		return errors.New("Auto Q/R profile name cannot be empty")
	}
	if strings.EqualFold(newName, autoquery.DefaultProfileName) {
		return fmt.Errorf("Auto Q/R profile %q already exists", autoquery.DefaultProfileName)
	}
	profiles := normalizeAutoQueryProfiles(state.autoQueryProfiles)
	index := -1
	for i, profile := range profiles {
		if strings.EqualFold(strings.TrimSpace(profile.Name), current) {
			index = i
			continue
		}
		if strings.EqualFold(strings.TrimSpace(profile.Name), newName) {
			return fmt.Errorf("Auto Q/R profile %q already exists", newName)
		}
	}
	if index < 0 {
		return fmt.Errorf("Auto Q/R profile %q not found", current)
	}
	profiles[index].Name = newName
	state.autoQueryProfiles = profiles
	state.autoQueryProfileName = newName
	return nil
}

func saveAutoQueryProfileList(state *uiState) error {
	if state == nil || state.autoQueryProfileStore == nil {
		return nil
	}
	state.autoQueryProfiles = normalizeAutoQueryProfiles(state.autoQueryProfiles)
	return state.autoQueryProfileStore.Save(state.autoQueryProfiles)
}

func autoQuerySourceFromNode(node nodes.Node, enabled bool) autoquery.Source {
	return autoquery.Source{
		NodeID:  strings.TrimSpace(node.ID),
		Name:    strings.TrimSpace(node.Name),
		Host:    strings.TrimSpace(node.Host),
		Port:    node.Port,
		Enabled: enabled,
	}
}

func autoQuerySourceKey(source autoquery.Source) string {
	if strings.TrimSpace(source.NodeID) != "" {
		return "id:" + strings.TrimSpace(source.NodeID)
	}
	return fmt.Sprintf("endpoint:%s:%s:%d", strings.TrimSpace(source.Name), strings.TrimSpace(source.Host), source.Port)
}

func autoQueryNodeKey(node nodes.Node) string {
	if strings.TrimSpace(node.ID) != "" {
		return "id:" + strings.TrimSpace(node.ID)
	}
	return fmt.Sprintf("endpoint:%s:%s:%d", strings.TrimSpace(node.Name), strings.TrimSpace(node.Host), node.Port)
}

func autoQuerySourcesForNodes(sources []autoquery.Source, nodeList []nodes.Node) []autoquery.Source {
	if len(nodeList) == 0 {
		return nil
	}
	indexByKey := make(map[string]int, len(nodeList))
	for index, node := range nodeList {
		indexByKey[autoQueryNodeKey(node)] = index
	}
	var out []autoquery.Source
	seen := make(map[string]bool, len(nodeList))
	for _, source := range sources {
		key := autoQuerySourceKey(source)
		index, ok := indexByKey[key]
		if !ok || seen[key] {
			continue
		}
		out = append(out, autoQuerySourceFromNode(nodeList[index], source.Enabled))
		seen[key] = true
	}
	for _, node := range nodeList {
		key := autoQueryNodeKey(node)
		if seen[key] {
			continue
		}
		out = append(out, autoQuerySourceFromNode(node, querySourceChecked(node)))
		seen[key] = true
	}
	return out
}

func applyAutoQuerySources(state *uiState, sources []autoquery.Source) {
	if state == nil {
		return
	}
	state.autoQuerySources = autoQuerySourcesForNodes(sources, state.nodes)
	state.selectedAutoQuerySourceRow = -1
}

func autoQuerySourcesForState(state *uiState) []autoquery.Source {
	if state == nil {
		return nil
	}
	return autoQuerySourcesForNodes(state.autoQuerySources, state.nodes)
}

func autoQueryCriteriaForState(state *uiState) autoquery.Criteria {
	criteria := autoquery.Criteria{
		SearchField: queryQuickSearchPatientName,
		DatePreset:  queryDatePresetAny,
		RefreshMode: autoQueryRefreshEvery30Min,
	}
	if state == nil {
		return criteria
	}
	if stringInList(state.autoQuerySearchField, queryQuickSearchOptions) {
		criteria.SearchField = state.autoQuerySearchField
	}
	criteria.SearchText = strings.TrimSpace(state.autoQuerySearchText)
	if datePreset := normalizeQueryDatePreset(state.autoQueryDatePreset); stringInList(datePreset, queryDatePresetOptions) {
		criteria.DatePreset = datePreset
	}
	criteria.OnDate = strings.TrimSpace(state.autoQueryOnDate)
	criteria.LastHours = strings.TrimSpace(state.autoQueryLastHours)
	criteria.Modalities = normalizedAutoQueryModalities(state.autoQueryModalities)
	if stringInList(state.autoQueryRefreshMode, autoQueryRefreshModeOptions) {
		criteria.RefreshMode = state.autoQueryRefreshMode
	}
	return criteria
}

func applyAutoQueryCriteria(state *uiState, criteria autoquery.Criteria) {
	if state == nil {
		return
	}
	criteria.DatePreset = normalizeQueryDatePreset(criteria.DatePreset)
	if !stringInList(criteria.SearchField, queryQuickSearchOptions) {
		criteria.SearchField = queryQuickSearchPatientName
	}
	if !stringInList(criteria.DatePreset, queryDatePresetOptions) {
		criteria.DatePreset = queryDatePresetAny
	}
	if !stringInList(criteria.RefreshMode, autoQueryRefreshModeOptions) {
		criteria.RefreshMode = autoQueryRefreshEvery30Min
	}
	state.autoQuerySearchField = criteria.SearchField
	state.autoQuerySearchText = strings.TrimSpace(criteria.SearchText)
	state.autoQueryDatePreset = criteria.DatePreset
	state.autoQueryOnDate = strings.TrimSpace(criteria.OnDate)
	state.autoQueryLastHours = strings.TrimSpace(criteria.LastHours)
	state.autoQueryModalities = normalizedAutoQueryModalities(criteria.Modalities)
	state.autoQueryRefreshMode = criteria.RefreshMode
}

func autoQueryCriteriaFromControls(field string, search string, datePreset string, onDate string, lastHours string, modalityChecks map[string]*widget.Check, refreshMode string) autoquery.Criteria {
	return autoquery.Criteria{
		SearchField: field,
		SearchText:  strings.TrimSpace(search),
		DatePreset:  datePreset,
		OnDate:      strings.TrimSpace(onDate),
		LastHours:   strings.TrimSpace(lastHours),
		Modalities:  selectedQueryModalities(modalityChecks),
		RefreshMode: refreshMode,
	}
}

func autoQueryCriteriaEqual(lhs autoquery.Criteria, rhs autoquery.Criteria) bool {
	return lhs.SearchField == rhs.SearchField &&
		strings.TrimSpace(lhs.SearchText) == strings.TrimSpace(rhs.SearchText) &&
		lhs.DatePreset == rhs.DatePreset &&
		strings.TrimSpace(lhs.OnDate) == strings.TrimSpace(rhs.OnDate) &&
		strings.TrimSpace(lhs.LastHours) == strings.TrimSpace(rhs.LastHours) &&
		lhs.RefreshMode == rhs.RefreshMode &&
		strings.Join(normalizedAutoQueryModalities(lhs.Modalities), "\\") == strings.Join(normalizedAutoQueryModalities(rhs.Modalities), "\\")
}

func selectedQueryModalities(modalityChecks map[string]*widget.Check) []string {
	var selected []string
	for _, code := range queryModalityCodes {
		check := modalityChecks[code]
		if check != nil && check.Checked {
			selected = append(selected, code)
		}
	}
	return selected
}

func normalizedAutoQueryModalities(modalities []string) []string {
	wanted := make(map[string]bool, len(modalities))
	for _, modality := range modalities {
		modality = strings.ToUpper(strings.TrimSpace(modality))
		if modality != "" {
			wanted[modality] = true
		}
	}
	var normalized []string
	for _, code := range queryModalityCodes {
		if wanted[code] {
			normalized = append(normalized, code)
		}
	}
	return normalized
}

func autoQueryProfileFromState(state *uiState) autoquery.Profile {
	settings := autoQuerySettingsForState(state)
	return autoquery.Profile{
		Name:   selectedAutoQueryProfileName(state),
		Locked: autoQueryProfileLocked(state),
		Settings: autoquery.Settings{
			AutoRetrieve:        settings.AutoRetrieve,
			RetrieveLevel:       settings.RetrieveLevel,
			MaxMatches:          settings.MaxMatches,
			DuplicatePolicy:     settings.DuplicatePolicy,
			RequireConfirmation: settings.RequireConfirmation,
		},
		Criteria: autoQueryCriteriaForState(state),
		Sources:  autoQuerySourcesForState(state),
	}
}

func loadAutoQueryProfiles(state *uiState, profiles []autoquery.Profile) {
	if state == nil {
		return
	}
	state.autoQueryProfiles = normalizeAutoQueryProfiles(profiles)
	_ = selectAutoQueryProfile(state, autoquery.DefaultProfileName)
}

func saveAutoQueryDefaultProfile(state *uiState) error {
	return saveAutoQuerySelectedProfile(state)
}

func saveAutoQuerySelectedProfile(state *uiState) error {
	if state == nil || state.autoQueryProfileStore == nil {
		return nil
	}
	current := autoQueryProfileFromState(state)
	profiles := normalizeAutoQueryProfiles(state.autoQueryProfiles)
	replaced := false
	for i, profile := range profiles {
		if strings.EqualFold(strings.TrimSpace(profile.Name), current.Name) {
			profiles[i] = current
			replaced = true
			break
		}
	}
	if !replaced {
		profiles = append(profiles, current)
	}
	state.autoQueryProfiles = profiles
	return state.autoQueryProfileStore.Save(profiles)
}

func saveAutoQueryProfileCriteria(w fyne.Window, status *widget.Label, state *uiState, criteria autoquery.Criteria) bool {
	if autoQueryProfileLocked(state) {
		if autoQueryCriteriaEqual(criteria, autoQueryCriteriaForState(state)) {
			return true
		}
		if status != nil {
			status.SetText("Auto Q/R profile is locked")
		}
		return false
	}
	applyAutoQueryCriteria(state, criteria)
	if err := saveAutoQuerySelectedProfile(state); err != nil {
		if status != nil {
			status.SetText("Auto Q/R profile save failed")
		}
		if w != nil {
			dialog.ShowError(err, w)
		}
		return false
	}
	return true
}

func applyAutoQueryProfileRename(w fyne.Window, status *widget.Label, state *uiState, newName string) bool {
	if err := renameSelectedAutoQueryProfile(state, newName); err != nil {
		if status != nil {
			status.SetText(err.Error())
		}
		if w != nil {
			dialog.ShowError(err, w)
		}
		return false
	}
	if err := saveAutoQueryProfileList(state); err != nil {
		if status != nil {
			status.SetText("Auto Q/R profile rename save failed")
		}
		if w != nil {
			dialog.ShowError(err, w)
		}
		return false
	}
	if status != nil {
		status.SetText("Auto Q/R profile renamed")
	}
	return true
}

func stringInList(value string, list []string) bool {
	for _, item := range list {
		if value == item {
			return true
		}
	}
	return false
}

func autoQuerySettingsFormItems(state *uiState) []*widget.FormItem {
	items, _ := newAutoQuerySettingsForm(state)
	return items
}

func newAutoQuerySettingsForm(state *uiState) ([]*widget.FormItem, func()) {
	settings := autoQuerySettingsForState(state)
	retrieveLevel := widget.NewSelect(autoQueryRetrieveLevelOptions, nil)
	retrieveLevel.SetSelected(settings.RetrieveLevel)
	maxMatches := widget.NewEntry()
	maxMatches.SetText(settings.MaxMatches)
	maxMatches.SetPlaceHolder(autoQueryDefaultMaxMatches)
	duplicatePolicy := widget.NewSelect(autoQueryDuplicatePolicyOptions, nil)
	duplicatePolicy.SetSelected(settings.DuplicatePolicy)
	requireConfirmation := widget.NewCheck("", nil)
	requireConfirmation.SetChecked(settings.RequireConfirmation)
	items := []*widget.FormItem{
		widget.NewFormItem("Retrieve Level", retrieveLevel),
		widget.NewFormItem("Max Matches", maxMatches),
		widget.NewFormItem("Duplicate Policy", duplicatePolicy),
		widget.NewFormItem("Require Confirmation", requireConfirmation),
	}
	apply := func() {
		applyAutoQuerySettings(state, autoQuerySettings{
			AutoRetrieve:        settings.AutoRetrieve,
			AutoRetrieveSet:     true,
			RetrieveLevel:       retrieveLevel.Selected,
			MaxMatches:          maxMatches.Text,
			DuplicatePolicy:     duplicatePolicy.Selected,
			RequireConfirmation: requireConfirmation.Checked,
		})
	}
	return items, apply
}

func showAutoQuerySettingsDialog(w fyne.Window, status *widget.Label, state *uiState) {
	items, apply := newAutoQuerySettingsForm(state)
	if w == nil {
		apply()
		if err := saveAutoQueryDefaultProfile(state); err != nil {
			if status != nil {
				status.SetText("Auto Q/R settings save failed")
			}
			return
		}
		if status != nil {
			status.SetText("Auto Q/R settings saved")
		}
		return
	}
	form := dialog.NewForm("Auto Q/R Settings", "Save", "Cancel", items, func(ok bool) {
		if !ok {
			return
		}
		apply()
		if err := saveAutoQueryDefaultProfile(state); err != nil {
			if status != nil {
				status.SetText("Auto Q/R settings save failed")
			}
			dialog.ShowError(err, w)
			return
		}
		if status != nil {
			status.SetText("Auto Q/R settings saved")
		}
	}, w)
	form.Resize(fyne.NewSize(460, 0))
	form.Show()
}

func queryActionButtonLabels() []string {
	return []string{
		queryActionLabelQuery,
		queryActionLabelPatient,
		queryActionLabelRetrieve,
		queryActionLabelVerify,
	}
}

func queryAdvancedActionButtonLabels() []string {
	return []string{
		queryActionLabelSeries,
		queryActionLabelImages,
	}
}

func configureQueryQuickSearchPlaceholder(entry *widget.Entry, field string) {
	if entry == nil {
		return
	}
	switch field {
	case queryQuickSearchPatientName:
		entry.SetPlaceHolder("Patient Name")
	case queryQuickSearchCustomDICOMField:
		entry.SetPlaceHolder("StudyID=ABC123")
	case queryQuickSearchPatientID, queryQuickSearchAccession, queryQuickSearchBirthdate, queryQuickSearchDescription,
		queryQuickSearchReferringPhysician, queryQuickSearchInstitution, queryQuickSearchComments, queryQuickSearchStatus:
		entry.SetPlaceHolder(field)
	default:
		entry.SetPlaceHolder("Search")
	}
}

func newQueryQuickSearchFieldStrip(selectWidget *widget.Select, entry *widget.Entry) fyne.CanvasObject {
	buttons := map[string]*widget.Button{}
	highlights := map[string]*canvas.Rectangle{}
	refreshButtons := func() {
		if selectWidget == nil {
			return
		}
		for field, button := range buttons {
			button.Importance = widget.LowImportance
			if selectWidget.Selected == field {
				if highlight := highlights[field]; highlight != nil {
					highlight.FillColor = queryQuickSearchSelectedSegmentColor
					highlight.Refresh()
				}
			} else {
				if highlight := highlights[field]; highlight != nil {
					highlight.FillColor = color.Transparent
					highlight.Refresh()
				}
			}
			button.Refresh()
		}
	}
	if selectWidget != nil {
		previousOnChanged := selectWidget.OnChanged
		selectWidget.OnChanged = func(field string) {
			configureQueryQuickSearchPlaceholder(entry, field)
			if previousOnChanged != nil {
				previousOnChanged(field)
			}
			refreshButtons()
		}
	}
	objects := make([]fyne.CanvasObject, 0, len(queryQuickSearchOptions))
	for index, field := range queryQuickSearchOptions {
		field := field
		button := widget.NewButton(field, func() {
			if selectWidget != nil {
				selectWidget.SetSelected(field)
			}
		})
		button.Importance = widget.LowImportance
		buttons[field] = button
		highlight := canvas.NewRectangle(color.Transparent)
		highlight.CornerRadius = 5
		highlights[field] = highlight
		objects = append(objects, newQueryQuickSearchSegment(button, index < len(queryQuickSearchOptions)-1, highlight))
	}
	initialField := ""
	if selectWidget != nil {
		initialField = selectWidget.Selected
	}
	configureQueryQuickSearchPlaceholder(entry, initialField)
	refreshButtons()
	strip := container.NewStack(canvas.NewRectangle(archiveHeaderRowColor), container.NewHBox(objects...), newTableRowDividerLayer())
	return container.NewHScroll(strip)
}

func newQuerySearchBar(entry *widget.Entry, submit func(), fieldMenuTapped ...func(fyne.CanvasObject)) fyne.CanvasObject {
	bar, _ := newQuerySearchBarWithFieldMenuButton(entry, submit, fieldMenuTapped...)
	return bar
}

func newQuerySearchBarWithFieldMenuButton(entry *widget.Entry, submit func(), fieldMenuTapped ...func(fyne.CanvasObject)) (fyne.CanvasObject, *widget.Button) {
	if entry != nil {
		entry.OnSubmitted = func(_ string) {
			if submit != nil {
				submit()
			}
		}
	}
	submitButton := widget.NewButtonWithIcon("", theme.SearchIcon(), func() {
		if submit != nil {
			submit()
		}
	})
	submitButton.Importance = widget.LowImportance
	var fieldMenuButton *widget.Button
	fieldMenuButton = widget.NewButtonWithIcon("", theme.MenuDropDownIcon(), func() {
		if len(fieldMenuTapped) > 0 && fieldMenuTapped[0] != nil {
			fieldMenuTapped[0](fieldMenuButton)
		}
	})
	fieldMenuButton.Importance = widget.LowImportance
	if len(fieldMenuTapped) == 0 || fieldMenuTapped[0] == nil {
		fieldMenuButton.Disable()
	}
	entrySlot := fyne.CanvasObject(entry)
	if entry != nil {
		entrySlot = container.NewGridWrap(fyne.NewSize(querySearchBarEntryWidth, entry.MinSize().Height), entry)
	}
	return container.NewBorder(nil, nil, widget.NewIcon(theme.SearchIcon()), container.NewHBox(submitButton, fieldMenuButton), entrySlot), fieldMenuButton
}

func showQueryQuickSearchFieldMenu(anchor fyne.CanvasObject, selected string, choose func(string)) {
	if anchor == nil || fyne.CurrentApp() == nil {
		return
	}
	menuCanvas := fyne.CurrentApp().Driver().CanvasForObject(anchor)
	if menuCanvas == nil {
		return
	}
	menu := widget.NewPopUpMenu(newQueryQuickSearchFieldMenu(selected, choose), menuCanvas)
	menu.ShowAtRelativePosition(fyne.NewPos(0, anchor.MinSize().Height), anchor)
}

func newQueryQuickSearchFieldMenu(selected string, choose func(string)) *fyne.Menu {
	return fyne.NewMenu("Search Field", queryQuickSearchFieldMenuItems(selected, choose)...)
}

func queryQuickSearchFieldMenuItems(selected string, choose func(string)) []*fyne.MenuItem {
	items := make([]*fyne.MenuItem, 0, len(queryQuickSearchOptions))
	for _, option := range queryQuickSearchOptions {
		field := option
		item := fyne.NewMenuItem(field, func() {
			if choose != nil {
				choose(field)
			}
		})
		item.Checked = field == selected
		items = append(items, item)
	}
	return items
}

func newQueryQuickSearchSegment(content fyne.CanvasObject, divider bool, highlight *canvas.Rectangle) fyne.CanvasObject {
	size := content.MinSize()
	if size.Width < queryQuickSearchSegmentMinWidth {
		size.Width = queryQuickSearchSegmentMinWidth
	}
	size.Height = queryQuickSearchSegmentHeight
	content = container.NewGridWrap(size, content)
	if highlight != nil {
		highlight.CornerRadius = 5
		highlight.SetMinSize(size)
		highlightLayer := container.New(queryQuickSearchSegmentHighlightLayout{}, highlight)
		content = container.NewStack(highlightLayer, content)
	}
	if !divider {
		return content
	}
	return container.NewStack(content, newTableColumnDividerLayer())
}

type queryQuickSearchSegmentHighlightLayout struct{}

func (queryQuickSearchSegmentHighlightLayout) Layout(objects []fyne.CanvasObject, size fyne.Size) {
	width := size.Width - (queryQuickSearchSelectedSegmentHorizontalInset * 2)
	if width < 0 {
		width = 0
	}
	height := size.Height - (queryQuickSearchSelectedSegmentVerticalInset * 2)
	if height < 0 {
		height = 0
	}
	highlightSize := fyne.NewSize(width, height)
	for _, object := range objects {
		object.Move(fyne.NewPos(queryQuickSearchSelectedSegmentHorizontalInset, queryQuickSearchSelectedSegmentVerticalInset))
		object.Resize(highlightSize)
	}
}

func (queryQuickSearchSegmentHighlightLayout) MinSize(_ []fyne.CanvasObject) fyne.Size {
	return fyne.NewSize(0, 0)
}

func newQueryRefreshButton(tapped func()) *widget.Button {
	button := widget.NewButtonWithIcon("", theme.ViewRefreshIcon(), tapped)
	button.Importance = widget.LowImportance
	return button
}

func queryRefreshCadenceSlot(selectWidget *widget.Select) fyne.CanvasObject {
	return container.NewGridWrap(fyne.NewSize(queryRefreshCadenceSlotWidth, selectWidget.MinSize().Height), selectWidget)
}

func queryRefreshCountdownSlot(label *widget.Label) fyne.CanvasObject {
	return container.NewGridWrap(fyne.NewSize(queryRefreshCountdownSlotWidth, label.MinSize().Height), label)
}

func autoQueryRefreshCountdownSlot(label *widget.Label) fyne.CanvasObject {
	return container.NewGridWrap(fyne.NewSize(autoQueryRefreshCountdownSlotWidth, label.MinSize().Height), label)
}

func queryRefreshButtonSlot(button *widget.Button) fyne.CanvasObject {
	return autoQueryRefreshButtonSlot(button)
}

func autoQueryRefreshButtonSlot(button *widget.Button) fyne.CanvasObject {
	if button == nil {
		return container.NewGridWrap(fyne.NewSize(autoQueryProfileIconSlotSize, autoQueryProfileIconSlotSize), canvas.NewRectangle(color.Transparent))
	}
	return container.NewGridWrap(fyne.NewSize(autoQueryProfileIconSlotSize, autoQueryProfileIconSlotSize), button)
}

func queryAutoRetrieveSlot(check *widget.Check) fyne.CanvasObject {
	return container.NewGridWrap(fyne.NewSize(queryAutoRetrieveSlotWidth, check.MinSize().Height), check)
}

func queryAutoRetrieveSettingsSlot(button *widget.Button) fyne.CanvasObject {
	return container.NewGridWrap(fyne.NewSize(queryAutoRetrieveSettingsSlotWidth, button.MinSize().Height), button)
}

func autoQueryRetrieveSettingsSlot(button *widget.Button) fyne.CanvasObject {
	return container.NewGridWrap(fyne.NewSize(autoQueryRetrieveSettingsSlotWidth, button.MinSize().Height), button)
}

func newQueryCriteriaViewport(criteria fyne.CanvasObject) *container.Scroll {
	return container.NewVScroll(criteria)
}

func newQueryWorkspace(criteria fyne.CanvasObject, results fyne.CanvasObject) fyne.CanvasObject {
	return container.New(queryWorkspaceLayout{}, newQueryCriteriaViewport(criteria), results)
}

func mainToolbarButtonLabels() []string {
	var labels []string
	for _, group := range mainToolbarButtonGroups() {
		labels = append(labels, group...)
	}
	return labels
}

func mainToolbarButtonGroups() [][]string {
	return [][]string{
		{
			toolbarLabelImport,
			toolbarLabelExport,
		},
		{
			toolbarLabelQuery,
			toolbarLabelSendStudy,
		},
		{
			toolbarLabelAnonymize,
			toolbarLabelMetaData,
			toolbarLabelDelete,
		},
		{
			toolbarLabelOpen,
			toolbarLabelInspect,
			toolbarLabelFolder,
			toolbarLabelRefresh,
		},
		{
			toolbarLabelSendSeries,
			toolbarLabelSendImage,
			toolbarLabelRetrieveSeries,
			toolbarLabelRetrieveImage,
			toolbarLabelCancel,
		},
		{
			toolbarLabelAdd,
			toolbarLabelEdit,
			toolbarLabelVerify,
		},
		{
			toolbarLabelListen,
			toolbarLabelStop,
			toolbarLabelSettings,
		},
	}
}

func mainToolbarDisabledLabels() []string {
	return nil
}

func mainToolbarIconResource(label string) fyne.Resource {
	switch label {
	case toolbarLabelImport, toolbarLabelQuery:
		return mainToolbarTransferDownIconResource
	case toolbarLabelExport, toolbarLabelSendStudy:
		return mainToolbarTransferUpIconResource
	case toolbarLabelRetrieveSeries, toolbarLabelRetrieveImage, toolbarLabelListen:
		return theme.DownloadIcon()
	case toolbarLabelSendSeries, toolbarLabelSendImage:
		return theme.UploadIcon()
	case toolbarLabelOpen:
		return theme.FolderOpenIcon()
	case toolbarLabelInspect:
		return theme.SearchIcon()
	case toolbarLabelFolder:
		return theme.FolderIcon()
	case toolbarLabelRefresh:
		return theme.ViewRefreshIcon()
	case toolbarLabelCancel, toolbarLabelStop:
		return theme.MediaStopIcon()
	case toolbarLabelAnonymize:
		return mainToolbarAnonymizeIconResource
	case toolbarLabelAdd:
		return theme.ContentAddIcon()
	case toolbarLabelEdit:
		return theme.DocumentCreateIcon()
	case toolbarLabelDelete:
		return mainToolbarDeleteIconResource
	case toolbarLabelVerify:
		return theme.ConfirmIcon()
	case toolbarLabelMetaData:
		return mainToolbarMetadataIconResource
	case toolbarLabelSettings:
		return theme.SettingsIcon()
	default:
		return theme.InfoIcon()
	}
}

func compactToolbarButton(label string, icon fyne.Resource, tapped func()) *widget.Button {
	button := widget.NewButtonWithIcon(label, icon, tapped)
	button.Importance = widget.LowImportance
	return button
}

const (
	mainToolbarActionIconSlotSize     float32 = 44
	mainToolbarActionSlotWidth        float32 = 76
	mainToolbarActionCaptionSlotWidth float32 = mainToolbarActionSlotWidth
	mainToolbarGroupSeparatorWidth    float32 = 12
	mainToolbarGroupSeparatorHeight   float32 = 70
)

func mainToolbarAction(label string, icon fyne.Resource, tapped func()) *fyne.Container {
	button := widget.NewButtonWithIcon("", icon, tapped)
	button.Importance = widget.LowImportance
	iconSlot := container.NewGridWrap(fyne.NewSize(mainToolbarActionIconSlotSize, mainToolbarActionIconSlotSize), button)
	caption := mainToolbarCaptionLabel()
	caption.SetText(label)
	captionSlot := container.NewGridWrap(fyne.NewSize(mainToolbarActionCaptionSlotWidth, caption.MinSize().Height), caption)
	return container.NewVBox(iconSlot, captionSlot)
}

func mainToolbarCaptionLabel() *widget.Label {
	label := widget.NewLabel("")
	label.Wrapping = fyne.TextTruncate
	label.Alignment = fyne.TextAlignCenter
	return label
}

func disabledToolbarButton(label string, icon fyne.Resource) *widget.Button {
	button := compactToolbarButton(label, icon, nil)
	button.Disable()
	return button
}

func disabledMainToolbarAction(label string, icon fyne.Resource) *fyne.Container {
	action := mainToolbarAction(label, icon, nil)
	if button := findToolbarActionButton(action); button != nil {
		button.Disable()
	}
	return action
}

func findToolbarActionButton(action fyne.CanvasObject) *widget.Button {
	if button, ok := action.(*widget.Button); ok {
		return button
	}
	if c, ok := action.(*fyne.Container); ok {
		for _, child := range c.Objects {
			if button := findToolbarActionButton(child); button != nil {
				return button
			}
		}
	}
	return nil
}

func mainToolbarGroupSeparator() fyne.CanvasObject {
	return container.NewGridWrap(fyne.NewSize(mainToolbarGroupSeparatorWidth, mainToolbarGroupSeparatorHeight), widget.NewSeparator())
}

func groupedToolbarActions(buttons map[string]fyne.CanvasObject) *fyne.Container {
	objects := []fyne.CanvasObject{}
	for groupIndex, group := range mainToolbarButtonGroups() {
		if groupIndex > 0 {
			objects = append(objects, mainToolbarGroupSeparator())
		}
		for _, label := range group {
			if button := buttons[label]; button != nil {
				objects = append(objects, button)
			}
		}
	}
	return container.NewStack(
		canvas.NewRectangle(archiveHeaderRowColor),
		newCompactTableCellContent(container.NewHBox(objects...)),
		newTableColumnDividerLayer(),
		newTableRowDividerLayer(),
	)
}

func newMainToolbar(status *widget.Label, actions fyne.CanvasObject, toolbarSearch fyne.CanvasObject) fyne.CanvasObject {
	return container.NewVBox(container.NewBorder(nil, nil, nil, toolbarSearch, actions), status)
}

func selectAppTabByText(tabs *container.AppTabs, text string) bool {
	if tabs == nil {
		return false
	}
	for _, item := range tabs.Items {
		if item.Text == text {
			tabs.Select(item)
			return true
		}
	}
	return false
}

func openMetadataInspector(tabs *container.AppTabs, status *widget.Label, state *uiState, inspectSelected func()) {
	if !selectAppTabByText(tabs, "Inspector") {
		return
	}
	if _, ok := selectedInstance(state); ok {
		if inspectSelected != nil {
			inspectSelected()
		}
		return
	}
	if status != nil {
		status.SetText("Showing metadata inspector")
	}
}

var archiveQuickSearchOptions = []string{
	archiveQuickSearchPatientName,
	archiveQuickSearchPatientID,
	archiveQuickSearchAccession,
}

func archiveQuickSearchModeText(field string) string {
	field = strings.TrimSpace(field)
	return "Search by " + archiveQuickSearchPlaceholder(field)
}

func archiveQuickSearchPlaceholder(field string) string {
	field = strings.TrimSpace(field)
	if !stringInList(field, archiveQuickSearchOptions) {
		return archiveQuickSearchPatientName
	}
	return field
}

func main() {
	run()
}

func configureAppAppearance(a fyne.App) {
	if a == nil {
		return
	}
	a.Settings().SetTheme(newWorkstationCompactTheme())
}

func defaultWindowSize() fyne.Size {
	return fyne.NewSize(defaultWindowWidth, defaultWindowHeight)
}

type workstationCompactTheme struct {
	base fyne.Theme
}

func newWorkstationCompactTheme() fyne.Theme {
	return workstationCompactTheme{base: theme.DarkTheme()}
}

func (t workstationCompactTheme) Color(name fyne.ThemeColorName, variant fyne.ThemeVariant) color.Color {
	return t.base.Color(name, variant)
}

func (t workstationCompactTheme) Font(style fyne.TextStyle) fyne.Resource {
	return t.base.Font(style)
}

func (t workstationCompactTheme) Icon(name fyne.ThemeIconName) fyne.Resource {
	return t.base.Icon(name)
}

func (t workstationCompactTheme) Size(name fyne.ThemeSizeName) float32 {
	size := t.base.Size(name)
	switch name {
	case theme.SizeNameInputBorder, theme.SizeNameSeparatorThickness:
		return size
	case theme.SizeNameInnerPadding:
		return workstationInnerPadding
	case theme.SizeNamePadding:
		return workstationPadding
	default:
		return compactThemeSize(size)
	}
}

func compactThemeSize(size float32) float32 {
	if size <= 1 {
		return size
	}
	scaled := size * workstationCompactScale
	rounded := float32(int(scaled + 0.5))
	if rounded < 1 {
		return 1
	}
	return rounded
}

func run() {
	archiveDir := flag.String("archive-dir", defaultArchiveDir(), "directory for the local archive catalog and object store")
	flag.Parse()

	a := app.NewWithID("com.thalesmms.gopacs")
	configureAppAppearance(a)
	w := a.NewWindow("go-pacs")
	w.Resize(defaultWindowSize())

	session, err := core.Open(*archiveDir)
	if err != nil {
		dialog.ShowError(err, w)
		return
	}
	defer session.Close()

	appCfg, err := session.LoadConfig()
	if err != nil {
		dialog.ShowError(err, w)
		return
	}
	operationHistory, err := session.LoadHistory()
	if err != nil {
		dialog.ShowError(err, w)
		return
	}
	nodeList, err := session.ListNodes()
	if err != nil {
		dialog.ShowError(err, w)
		return
	}
	autoQueryProfiles, err := session.ListAutoQueryProfiles()
	if err != nil {
		dialog.ShowError(err, w)
		return
	}

	state := &uiState{
		session:                session,
		catalog:                session.Catalog(),
		nodeStore:              session.NodeStore(),
		autoQueryProfileStore:  session.AutoQueryStore(),
		nodes:                  nodeList,
		appConfig:              appCfg,
		appConfigPath:          session.ConfigPath(),
		operations:             operationHistory,
		operationHistoryPath:   session.HistoryPath(),
		openedArchiveStudyUIDs: openedArchiveStudyUIDMap(appCfg.OpenedArchiveStudyUIDs),
	}
	loadAutoQueryProfiles(state, autoQueryProfiles)
	applySavedNodeSortPreference(state)
	applySavedTaskSortPreference(state)
	status := widget.NewLabel("Ready")
	status.Wrapping = fyne.TextTruncate

	summary := newSummaryPanel()
	elementTable := newElementTable(state)
	studyTable := newStudyTable(state)
	seriesTable := newSeriesTable(state)
	instanceTable := newInstanceTable(state)
	taskTable := newTaskTable(state)
	taskDetail := newTaskDetail()
	state.operationTable = taskTable
	state.operationDetail = taskDetail
	updateTaskDetail(state)
	tables := archiveTables{
		studies:   studyTable,
		series:    seriesTable,
		instances: instanceTable,
	}
	wireArchiveTables(w, status, tables, state)
	nodeTable := newNodeTable(status, state)
	queryTab := newQueryTab(w, status, tables, nodeTable, state)
	autoQueryTab := newAutoQueryTab(w, status, tables, nodeTable, state)
	archiveControlSet := newArchiveControlSet(w, status, tables, state)
	archiveControls := archiveControlSet.archiveControls
	var tabs *container.AppTabs

	openButton := mainToolbarAction(toolbarLabelOpen, mainToolbarIconResource(toolbarLabelOpen), func() {
		openFileDialog(w, status, summary, elementTable, state)
	})
	inspectArchiveButton := mainToolbarAction(toolbarLabelInspect, mainToolbarIconResource(toolbarLabelInspect), func() {
		inspectSelectedArchiveInstance(w, status, summary, elementTable, state)
	})
	importFileButton := mainToolbarAction(toolbarLabelImport, mainToolbarIconResource(toolbarLabelImport), func() {
		importFileDialog(w, status, tables, state)
	})
	exportButton := mainToolbarAction(toolbarLabelExport, mainToolbarIconResource(toolbarLabelExport), func() {
		exportStudiesCSV(w, status, state)
	})
	importFolderButton := mainToolbarAction(toolbarLabelFolder, mainToolbarIconResource(toolbarLabelFolder), func() {
		importFolderDialog(w, status, tables, state)
	})
	refreshButton := mainToolbarAction(toolbarLabelRefresh, mainToolbarIconResource(toolbarLabelRefresh), func() {
		refreshStudies(w, status, tables, state)
	})
	queryButton := mainToolbarAction(toolbarLabelQuery, mainToolbarIconResource(toolbarLabelQuery), func() {
		if selectAppTabByText(tabs, "Query") {
			status.SetText("Showing Query workspace")
		}
	})
	sendStudyButton := mainToolbarAction(toolbarLabelSendStudy, mainToolbarIconResource(toolbarLabelSendStudy), func() {
		sendSelectedStudy(w, status, state)
	})
	sendSeriesButton := mainToolbarAction(toolbarLabelSendSeries, mainToolbarIconResource(toolbarLabelSendSeries), func() {
		sendSelectedSeries(w, status, state)
	})
	sendImageButton := mainToolbarAction(toolbarLabelSendImage, mainToolbarIconResource(toolbarLabelSendImage), func() {
		sendSelectedInstance(w, status, state)
	})
	retrieveSeriesButton := mainToolbarAction(toolbarLabelRetrieveSeries, mainToolbarIconResource(toolbarLabelRetrieveSeries), func() {
		retrieveSelectedSeries(w, status, tables, state)
	})
	retrieveImageButton := mainToolbarAction(toolbarLabelRetrieveImage, mainToolbarIconResource(toolbarLabelRetrieveImage), func() {
		retrieveSelectedInstance(w, status, tables, state)
	})
	cancelRetrieveButton := mainToolbarAction(toolbarLabelCancel, mainToolbarIconResource(toolbarLabelCancel), func() {
		cancelActiveRetrieve(status, state)
	})
	anonymizeButton := mainToolbarAction(toolbarLabelAnonymize, mainToolbarIconResource(toolbarLabelAnonymize), func() {
		showAnonymizeStudyDialog(w, status, tables, state)
	})
	metaDataButton := mainToolbarAction(toolbarLabelMetaData, mainToolbarIconResource(toolbarLabelMetaData), func() {
		openMetadataInspector(tabs, status, state, func() {
			inspectSelectedArchiveInstance(w, status, summary, elementTable, state)
		})
	})
	addNodeButton := mainToolbarAction(toolbarLabelAdd, mainToolbarIconResource(toolbarLabelAdd), func() {
		showAddNodeDialog(w, status, nodeTable, state)
	})
	editNodeButton := mainToolbarAction(toolbarLabelEdit, mainToolbarIconResource(toolbarLabelEdit), func() {
		showEditNodeDialog(w, status, nodeTable, state)
	})
	deleteStudyButton := mainToolbarAction(toolbarLabelDelete, mainToolbarIconResource(toolbarLabelDelete), func() {
		showDeleteStudyDialog(w, status, tables, state)
	})
	echoButton := mainToolbarAction(toolbarLabelVerify, mainToolbarIconResource(toolbarLabelVerify), func() {
		verifySelectedNode(w, status, nodeTable, state)
	})
	startReceiverButton := mainToolbarAction(toolbarLabelListen, mainToolbarIconResource(toolbarLabelListen), func() {
		startReceiver(w, status, state)
	})
	stopReceiverButton := mainToolbarAction(toolbarLabelStop, mainToolbarIconResource(toolbarLabelStop), func() {
		stopReceiver(w, status, tables, state)
	})
	settingsButton := mainToolbarAction(toolbarLabelSettings, mainToolbarIconResource(toolbarLabelSettings), func() {
		showSettingsDialog(w, status, tables, state)
	})

	actions := groupedToolbarActions(map[string]fyne.CanvasObject{
		toolbarLabelOpen:           openButton,
		toolbarLabelInspect:        inspectArchiveButton,
		toolbarLabelImport:         importFileButton,
		toolbarLabelExport:         exportButton,
		toolbarLabelFolder:         importFolderButton,
		toolbarLabelRefresh:        refreshButton,
		toolbarLabelQuery:          queryButton,
		toolbarLabelSendStudy:      sendStudyButton,
		toolbarLabelSendSeries:     sendSeriesButton,
		toolbarLabelSendImage:      sendImageButton,
		toolbarLabelRetrieveSeries: retrieveSeriesButton,
		toolbarLabelRetrieveImage:  retrieveImageButton,
		toolbarLabelCancel:         cancelRetrieveButton,
		toolbarLabelAnonymize:      anonymizeButton,
		toolbarLabelMetaData:       metaDataButton,
		toolbarLabelAdd:            addNodeButton,
		toolbarLabelEdit:           editNodeButton,
		toolbarLabelDelete:         deleteStudyButton,
		toolbarLabelVerify:         echoButton,
		toolbarLabelListen:         startReceiverButton,
		toolbarLabelStop:           stopReceiverButton,
		toolbarLabelSettings:       settingsButton,
	})
	actionsScroll := container.NewHScroll(actions)
	actionsScroll.SetMinSize(fyne.NewSize(0, actions.MinSize().Height))
	toolbar := newMainToolbar(status, actionsScroll, archiveControlSet.toolbarSearch)

	state.archiveSeriesSummary = compactWorkbenchLabel()
	state.archiveInstancesSummary = compactWorkbenchLabel()
	archiveBrowser := newArchiveBrowser(studyTable, seriesTable, instanceTable, state)
	archiveTab := newArchiveWorkbench(w, status, tables, archiveControls, archiveBrowser, state)
	networkTab := newNetworkTab(w, status, nodeTable, state)
	tasksBrowser := container.NewVSplit(
		labeledTable("Tasks", taskTable),
		container.NewStack(taskDetail),
	)
	tasksBrowser.SetOffset(0.55)
	tasksTab := container.NewBorder(nil, nil, nil, nil, tasksBrowser)
	inspectorTab := container.NewBorder(
		summary.container,
		nil,
		nil,
		nil,
		container.NewStack(elementTable),
	)
	tabs = container.NewAppTabs(
		container.NewTabItemWithIcon("Archive", theme.StorageIcon(), archiveTab),
		container.NewTabItemWithIcon(networkTabTitle, theme.ComputerIcon(), networkTab),
		container.NewTabItemWithIcon("Query", theme.SearchReplaceIcon(), queryTab),
		container.NewTabItemWithIcon(autoQueryTabTitle, theme.HistoryIcon(), autoQueryTab),
		container.NewTabItemWithIcon("Tasks", theme.HistoryIcon(), tasksTab),
		container.NewTabItemWithIcon("Inspector", theme.SearchIcon(), inspectorTab),
	)
	registerNetworkDeleteShortcut(w, tabs, status, nodeTable, state)
	registerNetworkVerifyShortcut(w, tabs, status, nodeTable, state)
	registerQueryShortcuts(w, tabs, status, tables, state)

	content := container.NewBorder(
		toolbar,
		nil,
		nil,
		nil,
		tabs,
	)
	w.SetContent(content)
	refreshStudies(w, status, tables, state)
	if state.appConfig.ReceiverAutoStart {
		startReceiver(w, status, state)
	}
	w.ShowAndRun()
}

type uiState struct {
	session                         *core.Session
	elements                        []dicominspect.ElementSummary
	studies                         []archive.Study
	archiveRows                     []archiveBrowserRow
	collapsedPatientGroups          map[string]bool
	collapsedArchiveStudies         map[string]bool
	collapsedArchiveSeries          map[string]bool
	archiveSeriesByStudy            map[string][]archive.Series
	archiveInstancesBySeries        map[string][]archive.Instance
	series                          []archive.Series
	instances                       []archive.Instance
	nodes                           []nodes.Node
	nodeTableRows                   []int
	queries                         []query.Match
	catalog                         *archive.Catalog
	nodeStore                       *nodes.Store
	autoQueryProfileStore           *autoquery.Store
	autoQueryProfiles               []autoquery.Profile
	autoQueryProfileName            string
	autoQueryProfileLocked          bool
	autoQuerySources                []autoquery.Source
	receiver                        *receive.Server
	appConfig                       appconfig.Config
	appConfigPath                   string
	operations                      []ops.Summary
	operationTable                  *widget.Table
	operationDetail                 *widget.Entry
	operationHistoryPath            string
	archiveAlbumList                *widget.List
	selectedArchiveAlbum            archiveAlbumID
	openedArchiveStudyUIDs          map[string]bool
	archiveSourceList               *widget.List
	archiveSourceMoveUpButton       *widget.Button
	archiveSourceMoveDownButton     *widget.Button
	archiveActivity                 *widget.Label
	archiveActivityList             *widget.List
	archiveClearActivityButton      *widget.Button
	archiveEditStudyButton          *widget.Button
	archiveSummaryTitle             *widget.Label
	archiveSummary                  *widget.Label
	archivePatientStudyList         *widget.List
	archiveResultSummary            *widget.Label
	archiveAlbumScopeLabel          *widget.Label
	archiveAdvancedFilterSync       func()
	archiveSeriesSummary            *widget.Label
	archiveInstancesSummary         *widget.Label
	archiveSelectedDetailsLabel     *widget.Label
	queryDestinationLabel           *widget.Label
	queryResultSummaryLabel         *widget.Label
	querySelectedDetailsLabel       *widget.Label
	querySourceHistoryLabel         *widget.Label
	queryCountdownLabel             *widget.Label
	autoQueryResultSummaryLabel     *widget.Label
	autoQueryCountdownLabel         *widget.Label
	querySourceList                 *widget.List
	queryMoveDestinationSelect      *widget.SelectEntry
	lastQuery                       lastQueryRequest
	autoQueryLast                   lastQueryRequest
	queryMoveDestination            string
	queryAutoRetrieve               bool
	queryKeepOnTop                  bool
	autoQueryAutoRetrieve           bool
	autoQueryRetrieveLevel          string
	autoQueryMaxMatches             string
	autoQueryDuplicatePolicy        string
	autoQueryRequireConfirmation    bool
	autoQuerySettingsConfigured     bool
	autoQuerySearchField            string
	autoQuerySearchText             string
	autoQueryDatePreset             string
	autoQueryOnDate                 string
	autoQueryLastHours              string
	autoQueryModalities             []string
	autoQueryRefreshMode            string
	autoQueryRefreshCancel          context.CancelFunc
	autoQueryNextRefresh            time.Time
	queryRefreshCancel              context.CancelFunc
	queryNextRefresh                time.Time
	nodeVerifyStatuses              map[string]nodeVerifyStatus
	nodeVerifyStatusTimes           map[string]time.Time
	querySourceStatuses             map[string]querySourceStatus
	querySourceStatusTimes          map[string]time.Time
	querySourceHistory              []sourceStatusHistoryEntry
	queryRetrieveRows               map[string]string
	receiverStartedAt               time.Time
	activeRetrieveCancel            context.CancelFunc
	retrieveActivityNode            string
	retrieveActivityLabel           string
	retrieveActivityProgress        retrieve.Progress
	activeQueryActivityLabel        string
	activeQueryActivityProgress     queryActivityProgress
	activeQueryActivityHasProgress  bool
	activeSendActivityLabel         string
	activeSendActivityProgress      send.Progress
	activeSendActivityHasProgress   bool
	activeImportActivityLabel       string
	activeImportActivityProgress    archive.ImportProgress
	activeImportActivityHasProgress bool
	selectedOperationRow            int
	studyFilters                    archive.StudyFilters
	seriesFilters                   archive.SeriesFilters
	selectedPatientKey              string
	archiveSelectRow                func(archiveBrowserRow)
	archiveToggleRow                func(archiveBrowserRow)
	selectedStudyRow                int
	selectedSeriesRow               int
	selectedInstanceRow             int
	selectedNodeRow                 int
	selectedAutoQuerySourceRow      int
	selectedQueryRow                int
	selectedQueryVirtual            bool
	selectedQueryVirtualMatch       query.Match
	collapsedQueryGroups            map[string]bool
	queryRunShortcutAction          func()
	archiveSortActive               bool
	archiveSortColumn               int
	archiveSortDescending           bool
	seriesSortActive                bool
	seriesSortColumn                int
	seriesSortDescending            bool
	instanceSortActive              bool
	instanceSortColumn              int
	instanceSortDescending          bool
	elementSortActive               bool
	elementSortColumn               int
	elementSortDescending           bool
	taskSortActive                  bool
	taskSortColumn                  int
	taskSortDescending              bool
	nodeSortActive                  bool
	nodeSortColumn                  int
	nodeSortDescending              bool
	querySortActive                 bool
	querySortColumn                 int
	querySortDescending             bool
}

type archiveTables struct {
	studies   *widget.Table
	series    *widget.Table
	instances *widget.Table
}

func recordOperation(state *uiState, summary ops.Summary) {
	if state == nil {
		return
	}
	state.operations = ops.Prepend(state.operations, summary)
	if state.operationHistoryPath != "" {
		_ = ops.SaveHistory(state.operationHistoryPath, state.operations)
	}
	state.selectedOperationRow = 0
	updateTaskDetail(state)
	if state.operationTable != nil {
		state.operationTable.Refresh()
	}
	refreshArchiveChrome(state)
}

type archiveActivityRow struct {
	Text                  string
	Detail                string
	OperationIndex        int
	Dismissible           bool
	Cancellable           bool
	ProgressVisible       bool
	ProgressValue         float64
	IndeterminateProgress bool
}

// queryActivityProgress is the GUI's local name for the frontend-agnostic
// progress value emitted by the multi-source query runner in internal/core.
type queryActivityProgress = core.QueryProgress

type sourceStatusHistoryEntry struct {
	At       time.Time
	NodeName string
	Kind     sourceStatusHistoryKind
	Status   string
}

func clearRecentOperations(status *widget.Label, state *uiState) {
	if state == nil {
		return
	}
	state.operations = nil
	state.selectedOperationRow = -1
	if state.operationHistoryPath != "" {
		if err := ops.SaveHistory(state.operationHistoryPath, state.operations); err != nil {
			if status != nil {
				status.SetText("Activity history clear failed")
			}
			return
		}
	}
	updateTaskDetail(state)
	if state.operationTable != nil {
		state.operationTable.Refresh()
	}
	refreshArchiveChrome(state)
	if status != nil {
		status.SetText("Activity history cleared")
	}
}

func dismissArchiveActivityRow(status *widget.Label, state *uiState, rowIndex int) {
	if state == nil {
		return
	}
	rows := archiveActivityRows(state)
	if rowIndex < 0 || rowIndex >= len(rows) || !rows[rowIndex].Dismissible {
		if status != nil {
			status.SetText("No dismissible activity row")
		}
		return
	}
	dismissOperationAt(status, state, rows[rowIndex].OperationIndex)
}

func dismissOperationAt(status *widget.Label, state *uiState, operationIndex int) {
	if state == nil || operationIndex < 0 || operationIndex >= len(state.operations) {
		if status != nil {
			status.SetText("No dismissible activity row")
		}
		return
	}
	state.operations, _ = ops.RemoveAt(state.operations, operationIndex)
	switch {
	case len(state.operations) == 0:
		state.selectedOperationRow = -1
	case state.selectedOperationRow > operationIndex:
		state.selectedOperationRow--
	case state.selectedOperationRow >= len(state.operations):
		state.selectedOperationRow = len(state.operations) - 1
	}
	if state.operationHistoryPath != "" {
		if err := ops.SaveHistory(state.operationHistoryPath, state.operations); err != nil {
			if status != nil {
				status.SetText("Activity row dismiss failed")
			}
			return
		}
	}
	updateTaskDetail(state)
	if state.operationTable != nil {
		state.operationTable.Refresh()
	}
	refreshArchiveChrome(state)
	if status != nil {
		status.SetText("Activity row dismissed")
	}
}

func newTaskDetail() *widget.Entry {
	detail := widget.NewMultiLineEntry()
	detail.Wrapping = fyne.TextWrapWord
	detail.Disable()
	return detail
}

const (
	taskTableColumnKind = iota
	taskTableColumnStatus
	taskTableColumnCounts
	taskTableColumnDuration
	taskTableColumnFailures
)

const taskSortPreferenceKey = "tasks"

func taskTableHeaders() []string {
	return []string{"Kind", "Status", "Counts", "Duration", "Failures"}
}

func newTaskTable(state *uiState) *widget.Table {
	headers := taskTableHeaders()
	var table *widget.Table
	table = widget.NewTable(
		func() (int, int) {
			return len(state.operations) + 1, len(headers)
		},
		func() fyne.CanvasObject {
			return newArchiveTableCell()
		},
		func(id widget.TableCellID, obj fyne.CanvasObject) {
			cell := obj.(*archiveTableCell)
			if id.Row == 0 {
				applyTextTableCell(cell, id.Row, taskHeaderLabel(state, id.Col, headers[id.Col]), true, false)
				return
			}
			summary := state.operations[id.Row-1]
			selected := id.Row-1 == state.selectedOperationRow
			applyTextTableCell(cell, id.Row, taskCell(summary, id.Col), false, selected)
		},
	)
	table.SetColumnWidth(0, 120)
	table.SetColumnWidth(1, 100)
	table.SetColumnWidth(2, 420)
	table.SetColumnWidth(3, 100)
	table.SetColumnWidth(4, 500)
	applyCompactTableRows(table)
	table.OnSelected = func(id widget.TableCellID) {
		if id.Row == 0 {
			if applyTaskSort(state, id.Col) {
				table.Refresh()
			}
			return
		}
		state.selectedOperationRow = id.Row - 1
		updateTaskDetail(state)
		table.Refresh()
	}
	return table
}

func applyTaskSort(state *uiState, col int) bool {
	if state == nil || !taskColumnSortable(col) {
		return false
	}
	if state.taskSortActive && state.taskSortColumn == col {
		state.taskSortDescending = !state.taskSortDescending
	} else {
		state.taskSortActive = true
		state.taskSortColumn = col
		state.taskSortDescending = false
	}
	sortTasksByColumn(state, col, state.taskSortDescending)
	persistTaskSortPreference(state)
	updateTaskDetail(state)
	return true
}

func applySavedTaskSortPreference(state *uiState) {
	if state == nil {
		return
	}
	pref, ok := state.appConfig.UISortPreferences[taskSortPreferenceKey]
	if !ok || !taskColumnSortable(pref.Column) {
		return
	}
	state.taskSortActive = true
	state.taskSortColumn = pref.Column
	state.taskSortDescending = pref.Descending
	sortTasksByColumn(state, pref.Column, pref.Descending)
}

func sortTasksByColumn(state *uiState, col int, descending bool) {
	if state == nil {
		return
	}
	var selected ops.Summary
	hadSelected := state.selectedOperationRow >= 0 && state.selectedOperationRow < len(state.operations)
	if hadSelected {
		selected = state.operations[state.selectedOperationRow]
	}
	sort.SliceStable(state.operations, func(i, j int) bool {
		less := taskSortLess(state.operations[i], state.operations[j], col)
		if descending {
			return taskSortLess(state.operations[j], state.operations[i], col)
		}
		return less
	})
	if hadSelected {
		state.selectedOperationRow = taskSummaryIndex(state.operations, selected)
	}
}

func persistTaskSortPreference(state *uiState) {
	if state == nil || state.appConfigPath == "" || !state.taskSortActive || !taskColumnSortable(state.taskSortColumn) {
		return
	}
	if state.appConfig.UISortPreferences == nil {
		state.appConfig.UISortPreferences = map[string]appconfig.SortPreference{}
	}
	state.appConfig.UISortPreferences[taskSortPreferenceKey] = appconfig.SortPreference{
		Column:     state.taskSortColumn,
		Descending: state.taskSortDescending,
	}
	_ = appconfig.Save(state.appConfigPath, state.appConfig)
}

func taskColumnSortable(col int) bool {
	return col >= taskTableColumnKind && col <= taskTableColumnFailures
}

func taskSortLess(left ops.Summary, right ops.Summary, col int) bool {
	switch col {
	case taskTableColumnDuration:
		if left.DurationMS != right.DurationMS {
			return left.DurationMS < right.DurationMS
		}
	default:
		leftText := taskSortValue(left, col)
		rightText := taskSortValue(right, col)
		if leftText != rightText {
			return leftText < rightText
		}
	}
	return taskSortTieValue(left) < taskSortTieValue(right)
}

func taskSortValue(summary ops.Summary, col int) string {
	return strings.ToLower(strings.TrimSpace(taskCell(summary, col)))
}

func taskSortTieValue(summary ops.Summary) string {
	return strings.Join([]string{
		string(summary.Kind),
		string(summary.Status),
		strconv.FormatUint(summary.DurationMS, 10),
		taskSortValue(summary, taskTableColumnFailures),
	}, "\x00")
}

func taskSummaryIndex(summaries []ops.Summary, selected ops.Summary) int {
	for i, summary := range summaries {
		if reflect.DeepEqual(summary, selected) {
			return i
		}
	}
	return -1
}

func sortHeaderLabel(label string, active bool, descending bool) string {
	if !active {
		return label
	}
	if descending {
		return label + " ▾"
	}
	return label + " ▴"
}

func taskHeaderLabel(state *uiState, col int, label string) string {
	if state == nil {
		return label
	}
	return sortHeaderLabel(label, state.taskSortActive && state.taskSortColumn == col, state.taskSortDescending)
}

func updateTaskDetail(state *uiState) {
	if state == nil || state.operationDetail == nil {
		return
	}
	if len(state.operations) == 0 {
		state.operationDetail.SetText("")
		return
	}
	if state.selectedOperationRow < 0 || state.selectedOperationRow >= len(state.operations) {
		state.selectedOperationRow = 0
	}
	state.operationDetail.SetText(taskDetailText(state.operations[state.selectedOperationRow]))
}

func taskDetailText(summary ops.Summary) string {
	data, err := json.MarshalIndent(summary, "", "  ")
	if err != nil {
		return err.Error()
	}
	return string(data)
}

func taskCell(summary ops.Summary, col int) string {
	switch col {
	case 0:
		return string(summary.Kind)
	case 1:
		return string(summary.Status)
	case 2:
		return countsCell(summary.Counts)
	case 3:
		return (time.Duration(summary.DurationMS) * time.Millisecond).String()
	case 4:
		if len(summary.Failures) == 0 {
			return ""
		}
		return summary.Failures[0].Message
	default:
		return ""
	}
}

func countsCell(counts ops.Counts) string {
	var parts []string
	appendCount := func(label string, value *uint64) {
		if value != nil && *value > 0 {
			parts = append(parts, fmt.Sprintf("%s %d", label, *value))
		}
	}
	appendCount("requested", counts.Requested)
	appendCount("matched", counts.Matched)
	appendCount("sent", counts.Sent)
	appendCount("received", counts.Received)
	appendCount("stored", counts.Stored)
	appendCount("duplicates", counts.Duplicates)
	appendCount("skipped", counts.Skipped)
	appendCount("failed", counts.Failed)
	if len(parts) == 0 {
		return ""
	}
	return strings.Join(parts, ", ")
}

func labeledTable(title string, table *widget.Table) fyne.CanvasObject {
	label := widget.NewLabelWithStyle(title, fyne.TextAlignLeading, fyne.TextStyle{Bold: true})
	label.Wrapping = fyne.TextTruncate
	return container.NewBorder(label, nil, nil, nil, container.NewStack(table))
}

func labeledTableWithFooter(title string, table *widget.Table, footer fyne.CanvasObject) fyne.CanvasObject {
	label := widget.NewLabelWithStyle(title, fyne.TextAlignLeading, fyne.TextStyle{Bold: true})
	label.Wrapping = fyne.TextTruncate
	return container.NewBorder(label, newArchiveFooter(footer), nil, nil, container.NewStack(table))
}

type summaryPanel struct {
	container *fyne.Container
	fields    map[string]*widget.Label
}

func newSummaryPanel() summaryPanel {
	fieldNames := []string{
		"File",
		"Patient",
		"Patient ID",
		"Study Date",
		"Modality",
		"Accession",
		"Study UID",
		"Series UID",
		"Series Number",
		"Series Description",
		"SOP Instance",
		"Instance Number",
		"Transfer Syntax",
	}
	fields := make(map[string]*widget.Label, len(fieldNames))
	cards := make([]fyne.CanvasObject, 0, len(fieldNames))
	for _, name := range fieldNames {
		value := widget.NewLabel("-")
		value.Wrapping = fyne.TextWrapBreak
		fields[name] = value
		cards = append(cards, widget.NewCard(name, "", value))
	}
	return summaryPanel{
		container: container.NewGridWithColumns(5, cards...),
		fields:    fields,
	}
}

func (p summaryPanel) set(summary dicominspect.Summary) {
	p.fields["File"].SetText(emptyDash(summary.FileName))
	p.fields["Patient"].SetText(emptyDash(summary.PatientName))
	p.fields["Patient ID"].SetText(emptyDash(summary.PatientID))
	p.fields["Study Date"].SetText(emptyDash(summary.StudyDate))
	p.fields["Modality"].SetText(emptyDash(summary.Modality))
	p.fields["Accession"].SetText(emptyDash(summary.AccessionNumber))
	p.fields["Study UID"].SetText(emptyDash(summary.StudyInstanceUID))
	p.fields["Series UID"].SetText(emptyDash(summary.SeriesInstanceUID))
	p.fields["Series Number"].SetText(emptyDash(summary.SeriesNumber))
	p.fields["Series Description"].SetText(emptyDash(summary.SeriesDescription))
	p.fields["SOP Instance"].SetText(emptyDash(summary.SOPInstanceUID))
	p.fields["Instance Number"].SetText(emptyDash(summary.InstanceNumber))
	p.fields["Transfer Syntax"].SetText(emptyDash(transferSyntaxLabel(summary)))
}

func emptyDash(value string) string {
	if strings.TrimSpace(value) == "" {
		return "-"
	}
	return value
}

func transferSyntaxLabel(summary dicominspect.Summary) string {
	if summary.TransferSyntax == "" {
		return summary.TransferSyntaxUID
	}
	if summary.TransferSyntaxUID == "" {
		return summary.TransferSyntax
	}
	return fmt.Sprintf("%s (%s)", summary.TransferSyntax, summary.TransferSyntaxUID)
}

func openFileDialog(w fyne.Window, status *widget.Label, summary summaryPanel, table *widget.Table, state *uiState) {
	picker := dialog.NewFileOpen(func(reader fyne.URIReadCloser, err error) {
		if err != nil {
			dialog.ShowError(err, w)
			return
		}
		if reader == nil {
			return
		}
		defer reader.Close()

		status.SetText("Inspecting " + reader.URI().Name())
		result, err := dicominspect.InspectReader(reader.URI().Name(), reader, dicominspect.DefaultOptions())
		if err != nil {
			status.SetText("Inspection failed")
			dialog.ShowError(err, w)
			return
		}
		state.elements = result.Elements
		applySavedElementSortPreference(state)
		summary.set(result)
		table.Refresh()
		status.SetText(fmt.Sprintf("%d elements loaded", result.ElementCount))
	}, w)
	picker.Show()
}

func inspectSelectedArchiveInstance(w fyne.Window, status *widget.Label, summary summaryPanel, table *widget.Table, state *uiState) {
	instance, ok := selectedInstance(state)
	if !ok {
		status.SetText("Select an image to inspect")
		return
	}
	if strings.TrimSpace(instance.StoredPath) == "" {
		status.SetText("Selected image has no stored path")
		return
	}
	label := instance.SOPInstanceUID
	if strings.TrimSpace(label) == "" || label == "(missing)" {
		label = filepath.Base(instance.StoredPath)
	}
	status.SetText("Inspecting archived image " + label)
	go func(path string) {
		result, err := dicominspect.InspectFile(path, dicominspect.DefaultOptions())
		fyne.Do(func() {
			if err != nil {
				status.SetText("Inspection failed")
				dialog.ShowError(err, w)
				return
			}
			state.elements = result.Elements
			applySavedElementSortPreference(state)
			summary.set(result)
			table.Refresh()
			status.SetText(fmt.Sprintf("%d elements loaded", result.ElementCount))
		})
	}(instance.StoredPath)
}

func newArchiveWorkbench(w fyne.Window, status *widget.Label, tables archiveTables, archiveControls fyne.CanvasObject, archiveBrowser fyne.CanvasObject, state *uiState) fyne.CanvasObject {
	sidebar := newArchiveSidebar(w, status, tables, state)
	summary := newArchiveSummaryPane(w, status, tables, state)
	state.archiveResultSummary = compactWorkbenchLabel()
	selectedDetails := widget.NewAccordion(widget.NewAccordionItem("Selected Archive Details", newArchiveSelectedDetailsPanel(state, status)))
	selectedDetails.CloseAll()
	centerFooter := container.NewVBox(selectedDetails, newArchiveFooter(state.archiveResultSummary))
	center := container.NewBorder(archiveControls, centerFooter, nil, nil, archiveBrowser)
	centerAndSummary := container.NewHSplit(center, summary)
	centerAndSummary.SetOffset(0.80)
	workbench := container.NewHSplit(sidebar, centerAndSummary)
	workbench.SetOffset(0.11)
	refreshArchiveChrome(state)
	return workbench
}

func newArchiveBrowser(studyTable *widget.Table, seriesTable *widget.Table, instanceTable *widget.Table, state *uiState) fyne.CanvasObject {
	// The Horos reference uses a single hierarchical patient/study/series/image
	// table plus the right-hand study pane. The study table already expands
	// series and instances inline, so the separate Series/Instances tables are
	// not stacked below it; they remain constructed and wired so selection,
	// sorting, and detail updates keep working off-screen.
	_ = seriesTable
	_ = instanceTable
	return container.NewStack(studyTable)
}

func newArchiveSidebar(w fyne.Window, status *widget.Label, tables archiveTables, state *uiState) fyne.CanvasObject {
	if state.selectedArchiveAlbum == "" {
		state.selectedArchiveAlbum = archiveAlbumDatabase
	}
	state.archiveAlbumList = newArchiveAlbumList(w, status, tables, state)
	state.archiveSourceList = newArchiveSourceList(state)
	state.archiveSourceMoveUpButton, state.archiveSourceMoveDownButton = newArchiveSourcePriorityButtons(w, status, state)
	state.archiveActivity = compactWorkbenchLabel()
	state.archiveActivity.Hide()
	state.archiveActivityList = newArchiveActivityList(status, state)
	state.archiveClearActivityButton = newActivityDismissButton(func() {
		clearRecentOperations(status, state)
	})
	state.archiveClearActivityButton.Hide()
	refreshArchiveSourcePriorityActions(state)
	content := container.NewVBox(
		newArchiveSidebarSection("Albums", nil, state.archiveAlbumList),
		newArchiveSidebarSection("Sources", container.NewHBox(state.archiveSourceMoveUpButton, state.archiveSourceMoveDownButton), state.archiveSourceList),
		newArchiveSidebarSection(
			"Activity",
			state.archiveClearActivityButton,
			state.archiveActivityList,
		),
	)
	scroll := container.NewVScroll(content)
	scroll.SetMinSize(fyne.NewSize(archiveSidebarMinWidth, 0))
	return scroll
}

func newArchiveFooter(content fyne.CanvasObject) fyne.CanvasObject {
	return container.NewStack(
		canvas.NewRectangle(archiveHeaderRowColor),
		newCompactTableCellContent(content),
		newTableColumnDividerLayer(),
		newTableRowDividerLayer(),
	)
}

func newArchiveSidebarSection(title string, actions fyne.CanvasObject, body ...fyne.CanvasObject) fyne.CanvasObject {
	header := container.NewBorder(nil, nil, nil, actions, workbenchCenteredTitle(title))
	headerChrome := container.NewStack(
		canvas.NewRectangle(archiveHeaderRowColor),
		newCompactTableCellContent(header),
		newTableRowDividerLayer(),
	)
	bodyChrome := container.NewStack(
		canvas.NewRectangle(archiveOddRowColor),
		newCompactTableCellContent(container.NewVBox(body...)),
		newTableRowDividerLayer(),
	)
	return container.NewStack(
		container.NewVBox(headerChrome, bodyChrome),
		newTableColumnDividerLayer(),
	)
}

func newArchiveSourcePriorityButtons(w fyne.Window, status *widget.Label, state *uiState) (*widget.Button, *widget.Button) {
	moveSource := func(delta int) {
		changed, err := moveQuerySource(state, delta)
		if err != nil {
			if status != nil {
				status.SetText("Source order update failed")
			}
			if w != nil {
				dialog.ShowError(err, w)
			}
			return
		}
		if changed {
			refreshArchiveChrome(state)
			refreshQueryDestination(state)
			refreshQueryResultSummary(state)
			refreshQuerySourceList(state)
			if status != nil {
				status.SetText("Updated source priority")
			}
		}
	}
	moveUpButton := widget.NewButtonWithIcon("", theme.MoveUpIcon(), func() {
		moveSource(-1)
	})
	moveDownButton := widget.NewButtonWithIcon("", theme.MoveDownIcon(), func() {
		moveSource(1)
	})
	moveUpButton.Importance = widget.LowImportance
	moveDownButton.Importance = widget.LowImportance
	return moveUpButton, moveDownButton
}

func refreshArchiveSourcePriorityActions(state *uiState) {
	if state == nil || state.archiveSourceMoveUpButton == nil || state.archiveSourceMoveDownButton == nil {
		return
	}
	if state.selectedNodeRow >= 0 && state.selectedNodeRow < len(state.nodes) {
		state.archiveSourceMoveUpButton.Show()
		state.archiveSourceMoveDownButton.Show()
		return
	}
	state.archiveSourceMoveUpButton.Hide()
	state.archiveSourceMoveDownButton.Hide()
}

type archiveSourceListItem struct {
	*fyne.Container
	background     *canvas.Rectangle
	sourceIcon     *widget.Icon
	sourceIconSlot *fyne.Container
	label          *widget.Label
}

func newArchiveSourceListItem() *archiveSourceListItem {
	sourceIcon := widget.NewIcon(archiveSourceLocalDBIconResource)
	sourceIconSlot := newArchiveRailIconSlot(sourceIcon)
	label := compactWorkbenchLabel()
	row := container.NewBorder(nil, nil, sourceIconSlot, nil, label)
	background := canvas.NewRectangle(archiveOddRowColor)
	content := container.NewStack(
		background,
		newCompactTableCellContent(row),
		newTableColumnDividerLayer(),
		newTableRowDividerLayer(),
	)
	return &archiveSourceListItem{
		Container:      content,
		background:     background,
		sourceIcon:     sourceIcon,
		sourceIconSlot: sourceIconSlot,
		label:          label,
	}
}

func newArchiveSourceList(state *uiState) *widget.List {
	list := widget.NewList(
		func() int {
			return len(archiveSourceRows(state))
		},
		func() fyne.CanvasObject {
			return newArchiveSourceListItem()
		},
		func(id widget.ListItemID, obj fyne.CanvasObject) {
			item := obj.(*archiveSourceListItem)
			rows := archiveSourceRows(state)
			if int(id) < 0 || int(id) >= len(rows) {
				item.label.SetText("")
				return
			}
			row := rows[id]
			item.label.SetText(row.Text)
			item.sourceIcon.SetResource(row.Icon)
			if row.Selected {
				item.background.FillColor = archiveSelectedRowColor
			} else {
				item.background.FillColor = archiveOddRowColor
			}
			item.background.Refresh()
		},
	)
	applyCompactArchiveRailListRows(list, len(archiveSourceRows(state)))
	list.OnSelected = func(id widget.ListItemID) {
		rows := archiveSourceRows(state)
		if id < 0 || id >= len(rows) {
			return
		}
		row := rows[id]
		if row.NodeIndex < 0 {
			if row.Selectable && state != nil {
				state.selectedNodeRow = -1
				refreshArchiveChrome(state)
				refreshQueryDestination(state)
				refreshQueryResultSummary(state)
				refreshQuerySourceList(state)
			} else {
				list.Unselect(id)
			}
			return
		}
		state.selectedNodeRow = row.NodeIndex
		refreshArchiveChrome(state)
		refreshQueryDestination(state)
		refreshQueryResultSummary(state)
		refreshQuerySourceList(state)
	}
	return list
}

type archiveAlbumListItem struct {
	*fyne.Container
	background    *canvas.Rectangle
	albumIcon     *widget.Icon
	albumIconSlot *fyne.Container
	label         *widget.Label
	count         *widget.Label
	countSlot     *fyne.Container
}

func newArchiveAlbumListItem() *archiveAlbumListItem {
	albumIcon := widget.NewIcon(theme.FolderIcon())
	albumIconSlot := newArchiveRailIconSlot(albumIcon)
	label := compactWorkbenchLabel()
	count := compactWorkbenchLabel()
	count.Alignment = fyne.TextAlignTrailing
	countSlot := container.NewGridWrap(fyne.NewSize(compactArchiveAlbumCountSlotWidth, count.MinSize().Height), count)
	countColumn := container.NewStack(countSlot, newTableLeadingColumnDividerLayer())
	row := container.NewBorder(nil, nil, albumIconSlot, countColumn, label)
	background := canvas.NewRectangle(archiveOddRowColor)
	content := container.NewStack(
		background,
		newCompactTableCellContent(row),
		newTableColumnDividerLayer(),
		newTableRowDividerLayer(),
	)
	return &archiveAlbumListItem{
		Container:     content,
		background:    background,
		albumIcon:     albumIcon,
		albumIconSlot: albumIconSlot,
		label:         label,
		count:         count,
		countSlot:     countSlot,
	}
}

func newArchiveAlbumList(w fyne.Window, status *widget.Label, tables archiveTables, state *uiState) *widget.List {
	list := widget.NewList(
		func() int {
			return len(archiveAlbumRowsForState(state, time.Now()))
		},
		func() fyne.CanvasObject {
			return newArchiveAlbumListItem()
		},
		func(id widget.ListItemID, obj fyne.CanvasObject) {
			rows := archiveAlbumRowsForState(state, time.Now())
			item := obj.(*archiveAlbumListItem)
			if id < 0 || id >= len(rows) {
				item.label.SetText("")
				item.count.SetText("")
				return
			}
			row := rows[id]
			item.label.SetText(archiveAlbumRailLabel(row.ID))
			item.count.SetText(strconv.Itoa(row.Count))
			item.albumIcon.SetResource(archiveAlbumIcon(row.ID))
			if row.Selected {
				item.background.FillColor = archiveSelectedRowColor
			} else {
				item.background.FillColor = archiveOddRowColor
			}
			item.background.Refresh()
		},
	)
	list.HideSeparators = true
	applyCompactArchiveRailListRows(list, len(archiveAlbumRowsForState(state, time.Now())))
	list.OnSelected = func(id widget.ListItemID) {
		rows := archiveAlbumRowsForState(state, time.Now())
		if id < 0 || id >= len(rows) {
			return
		}
		row := rows[id]
		if !row.Filterable {
			if status != nil {
				status.SetText(fmt.Sprintf("%s album is not available yet", row.Label))
			}
			list.Unselect(id)
			return
		}
		filters, ok := archiveFiltersWithAlbum(state.studyFilters, row.ID, time.Now())
		if !ok {
			list.Unselect(id)
			return
		}
		state.selectedArchiveAlbum = row.ID
		state.studyFilters = filters
		state.seriesFilters = archive.SeriesFilters{}
		refreshStudies(w, status, tables, state)
		list.Refresh()
	}
	return list
}

func newArchiveActivityList(status *widget.Label, state *uiState) *widget.List {
	list := widget.NewList(
		func() int {
			return len(archiveActivityRows(state))
		},
		func() fyne.CanvasObject {
			label := compactWorkbenchLabel()
			detail := compactWorkbenchDetailLabel()
			detail.Hide()
			textBlock := container.NewVBox(label, detail)
			progress := widget.NewProgressBar()
			progress.Hide()
			progressSlot := archiveActivityProgressSlot(progress)
			progressSlot.Hide()
			infinite := widget.NewProgressBarInfinite()
			infinite.Hide()
			infiniteSlot := archiveActivityProgressSlot(infinite)
			infiniteSlot.Hide()
			dismiss := newActivityDismissButton(nil)
			content := container.NewVBox(
				container.NewHBox(textBlock, layout.NewSpacer(), dismiss),
				progressSlot,
				infiniteSlot,
			)
			return container.NewStack(
				canvas.NewRectangle(archiveOddRowColor),
				newArchiveActivityRowContent(content),
				newTableColumnDividerLayer(),
				newTableRowDividerLayer(),
			)
		},
		func(id widget.ListItemID, obj fyne.CanvasObject) {
			rows := archiveActivityRows(state)
			stack := obj.(*fyne.Container)
			box := stack.Objects[1].(*fyne.Container).Objects[0].(*fyne.Container)
			header := box.Objects[0].(*fyne.Container)
			textBlock := header.Objects[0].(*fyne.Container)
			label := textBlock.Objects[0].(*widget.Label)
			detail := textBlock.Objects[1].(*widget.Label)
			dismiss := header.Objects[2].(*widget.Button)
			progressSlot := box.Objects[1].(*fyne.Container)
			progress := progressSlot.Objects[0].(*widget.ProgressBar)
			infiniteSlot := box.Objects[2].(*fyne.Container)
			infinite := infiniteSlot.Objects[0].(*widget.ProgressBarInfinite)
			if id < 0 || id >= len(rows) {
				label.SetText("")
				detail.SetText("")
				detail.Hide()
				dismiss.Hide()
				progressSlot.Hide()
				progress.Hide()
				infiniteSlot.Hide()
				infinite.Hide()
				return
			}
			row := rows[id]
			label.SetText(row.Text)
			if strings.TrimSpace(row.Detail) == "" {
				detail.SetText("")
				detail.Hide()
			} else {
				detail.SetText(row.Detail)
				detail.Show()
			}
			if row.ProgressVisible {
				progress.SetValue(row.ProgressValue)
				progress.Show()
				progressSlot.Show()
			} else {
				progress.SetValue(0)
				progress.Hide()
				progressSlot.Hide()
			}
			if row.IndeterminateProgress {
				infinite.Show()
				infiniteSlot.Show()
			} else {
				infinite.Hide()
				infiniteSlot.Hide()
			}
			if row.Cancellable {
				dismiss.OnTapped = func() {
					cancelActiveRetrieve(status, state)
				}
				dismiss.Show()
			} else if row.Dismissible {
				rowIndex := id
				dismiss.OnTapped = func() {
					dismissArchiveActivityRow(status, state, rowIndex)
				}
				dismiss.Show()
			} else {
				dismiss.OnTapped = nil
				dismiss.Hide()
			}
		},
	)
	list.HideSeparators = true
	applyCompactArchiveActivityRows(list, archiveActivityRows(state))
	return list
}

const (
	compactArchiveRailIconSlotSize      float32 = 20
	compactArchiveAlbumCountSlotWidth   float32 = 36
	compactArchiveActivityProgressWidth float32 = 184
	archiveSidebarMinWidth              float32 = 192
	archiveSummaryPaneMinWidth          float32 = 300
	archivePatientStudyMetricsSlotWidth float32 = 80
	archiveActivityVerticalPadding      float32 = 3
	compactArchiveRailListRowHeight     float32 = 28
	compactArchiveActivityRowHeight     float32 = 56
	compactArchivePatientStudyRowHeight float32 = 40
)

func newArchiveActivityRowContent(content fyne.CanvasObject) *fyne.Container {
	return container.New(
		layout.NewCustomPaddedLayout(archiveActivityVerticalPadding, archiveActivityVerticalPadding, tableCellHorizontalPadding, tableCellHorizontalPadding),
		content,
	)
}

func archiveActivityProgressSlot(progress fyne.CanvasObject) *fyne.Container {
	size := progress.MinSize()
	if size.Width < compactArchiveActivityProgressWidth {
		size.Width = compactArchiveActivityProgressWidth
	}
	return container.NewGridWrap(size, progress)
}

func newArchiveRailIconSlot(icon *widget.Icon) *fyne.Container {
	return container.NewGridWrap(fyne.NewSize(compactArchiveRailIconSlotSize, compactArchiveRailIconSlotSize), icon)
}

func applyCompactArchiveRailListRows(list *widget.List, count int) {
	applyListItemHeight(list, count, compactArchiveRailListRowHeight)
}

func applyCompactArchiveActivityRows(list *widget.List, rows []archiveActivityRow) {
	if list == nil {
		return
	}
	for id, row := range rows {
		height := compactArchiveRailListRowHeight
		if row.Cancellable || row.ProgressVisible || row.IndeterminateProgress || strings.TrimSpace(row.Detail) != "" {
			height = compactArchiveActivityRowHeight
		}
		list.SetItemHeight(widget.ListItemID(id), height)
	}
}

func applyCompactArchivePatientStudyRows(list *widget.List, count int) {
	applyListItemHeight(list, count, compactArchivePatientStudyRowHeight)
}

func applyListItemHeight(list *widget.List, count int, height float32) {
	if list == nil {
		return
	}
	for id := 0; id < count; id++ {
		list.SetItemHeight(widget.ListItemID(id), height)
	}
}

func newActivityDismissButton(tapped func()) *widget.Button {
	button := widget.NewButtonWithIcon("", theme.ContentClearIcon(), tapped)
	button.Importance = widget.LowImportance
	return button
}

func newArchiveSummaryPane(w fyne.Window, status *widget.Label, tables archiveTables, state *uiState) fyne.CanvasObject {
	state.archiveSummaryTitle = workbenchCenteredTitle("Selected Study")
	state.archiveSummary = compactWorkbenchLabel()
	state.archiveSummary.Wrapping = fyne.TextWrapWord
	state.archivePatientStudyList = newArchivePatientStudyList(state, tables)
	state.archiveEditStudyButton = widget.NewButtonWithIcon("", theme.DocumentCreateIcon(), func() {
		showStudyMetadataDialog(w, status, tables, state)
	})
	state.archiveEditStudyButton.Importance = widget.LowImportance
	state.archiveEditStudyButton.Disable()
	state.archiveEditStudyButton.Hide()
	header := container.NewBorder(nil, nil, nil, state.archiveEditStudyButton, state.archiveSummaryTitle)
	body := newArchiveSummaryPaneBody(state.archiveSummary, state.archivePatientStudyList)
	scroll := container.NewVScroll(body)
	scroll.SetMinSize(fyne.NewSize(archiveSummaryPaneMinWidth, 0))
	return newArchiveSummaryPaneChrome(header, scroll)
}

func newArchiveSummaryPaneBody(summary fyne.CanvasObject, patientStudyList fyne.CanvasObject) fyne.CanvasObject {
	// Show the selected-study metadata expanded by default so the widened right
	// pane reads like the Horos study panel instead of starting empty.
	details := widget.NewAccordion(widget.NewAccordionItem("Selected Study Details", summary))
	details.Open(0)
	return container.NewVBox(patientStudyList, details)
}

func newArchiveSummaryPaneChrome(header fyne.CanvasObject, body fyne.CanvasObject) fyne.CanvasObject {
	headerChrome := container.NewStack(
		canvas.NewRectangle(archiveHeaderRowColor),
		newCompactTableCellContent(header),
		newTableRowDividerLayer(),
	)
	bodyChrome := container.NewStack(
		canvas.NewRectangle(archiveOddRowColor),
		newCompactTableCellContent(body),
		newTableRowDividerLayer(),
	)
	return container.NewStack(
		container.NewVBox(headerChrome, bodyChrome),
		newTableColumnDividerLayer(),
	)
}

func compactWorkbenchLabel() *widget.Label {
	label := widget.NewLabel("")
	label.Wrapping = fyne.TextTruncate
	label.TextStyle.Monospace = true
	return label
}

func compactWorkbenchDetailLabel() *widget.Label {
	label := compactWorkbenchLabel()
	label.TextStyle = fyne.TextStyle{Monospace: true, Italic: true}
	return label
}

func workbenchSectionTitle(title string) *widget.Label {
	return widget.NewLabelWithStyle(title, fyne.TextAlignLeading, fyne.TextStyle{Bold: true})
}

func workbenchCenteredTitle(title string) *widget.Label {
	label := widget.NewLabelWithStyle(title, fyne.TextAlignCenter, fyne.TextStyle{Bold: true})
	label.Wrapping = fyne.TextTruncate
	return label
}

func workbenchWindowTitle(title string) fyne.CanvasObject {
	return workbenchWindowTitleLabel(workbenchCenteredTitle(title))
}

func workbenchWindowTitleLabel(label *widget.Label) fyne.CanvasObject {
	return container.NewStack(
		canvas.NewRectangle(archiveHeaderRowColor),
		newCompactTableCellContent(container.NewCenter(label)),
		newTableRowDividerLayer(),
	)
}

func workbenchPanel(title string, body fyne.CanvasObject) fyne.CanvasObject {
	header := container.NewStack(
		canvas.NewRectangle(archiveHeaderRowColor),
		newCompactTableCellContent(workbenchSectionTitle(title)),
		newTableRowDividerLayer(),
	)
	content := container.NewStack(
		canvas.NewRectangle(archiveOddRowColor),
		newCompactTableCellContent(body),
		newTableRowDividerLayer(),
	)
	return container.NewStack(
		container.NewVBox(header, content),
		newTableColumnDividerLayer(),
	)
}

func workbenchPanelSlot(title string, body fyne.CanvasObject, minWidth float32) fyne.CanvasObject {
	panel := workbenchPanel(title, body)
	size := panel.MinSize()
	if size.Width < minWidth {
		size.Width = minWidth
	}
	return container.NewGridWrap(size, panel)
}

func workbenchStrip(content fyne.CanvasObject) fyne.CanvasObject {
	return container.NewStack(
		canvas.NewRectangle(archiveHeaderRowColor),
		newCompactTableCellContent(content),
		newTableColumnDividerLayer(),
		newTableRowDividerLayer(),
	)
}

func refreshArchiveChrome(state *uiState) {
	if state == nil {
		return
	}
	if state.archiveAlbumList != nil {
		applyCompactArchiveRailListRows(state.archiveAlbumList, len(archiveAlbumRowsForState(state, time.Now())))
		state.archiveAlbumList.Refresh()
	}
	if state.archiveSourceList != nil {
		applyCompactArchiveRailListRows(state.archiveSourceList, len(archiveSourceRows(state)))
		state.archiveSourceList.Refresh()
	}
	refreshArchiveSourcePriorityActions(state)
	if state.archiveActivity != nil {
		state.archiveActivity.SetText(strings.Join(archiveActivityLines(state), "\n"))
	}
	if state.archiveActivityList != nil {
		applyCompactArchiveActivityRows(state.archiveActivityList, archiveActivityRows(state))
		state.archiveActivityList.Refresh()
	}
	if state.archiveClearActivityButton != nil {
		if len(state.operations) == 0 {
			state.archiveClearActivityButton.Hide()
		} else {
			state.archiveClearActivityButton.Show()
		}
	}
	if state.archiveEditStudyButton != nil {
		if _, ok := selectedStudy(state); ok && state.catalog != nil {
			state.archiveEditStudyButton.Show()
			state.archiveEditStudyButton.Enable()
		} else {
			state.archiveEditStudyButton.Disable()
			state.archiveEditStudyButton.Hide()
		}
	}
	if state.archiveSummary != nil {
		state.archiveSummary.SetText(archiveSummaryText(state))
	}
	if state.archiveSummaryTitle != nil {
		state.archiveSummaryTitle.SetText(archiveSummaryTitleText(state))
	}
	if state.archivePatientStudyList != nil {
		applyCompactArchivePatientStudyRows(state.archivePatientStudyList, len(patientStudySummaryRows(state, selectedArchiveStudyIndex(state))))
		state.archivePatientStudyList.Refresh()
	}
	if state.archiveResultSummary != nil {
		state.archiveResultSummary.SetText(archiveResultSummaryText(state))
	}
	if state.archiveAdvancedFilterSync != nil {
		state.archiveAdvancedFilterSync()
	}
	if state.archiveAlbumScopeLabel != nil {
		state.archiveAlbumScopeLabel.SetText(archiveAlbumScopeControlText(state.selectedArchiveAlbum))
	}
	if state.archiveSeriesSummary != nil {
		state.archiveSeriesSummary.SetText(archiveSeriesSummaryText(state))
	}
	if state.archiveInstancesSummary != nil {
		state.archiveInstancesSummary.SetText(archiveInstancesSummaryText(state))
	}
	refreshArchiveSelectedDetails(state)
}

func archiveSummaryTitleText(state *uiState) string {
	study, ok := selectedStudy(state)
	if !ok {
		return "Selected Study"
	}
	if patientName := displayPatientName(study.PatientName); patientName != "" {
		return patientName
	}
	if patientID := strings.TrimSpace(study.PatientID); patientID != "" {
		return patientID
	}
	return "Selected Study"
}

func archiveResultSummaryText(state *uiState) string {
	if state == nil || len(state.studies) == 0 {
		return "0 patients, 0 studies, 0 series, 0 images"
	}
	patientKeys := map[string]bool{}
	seriesCount := 0
	imageCount := 0
	for _, study := range state.studies {
		patientKeys[archivePatientKey(study)] = true
		seriesCount += study.SeriesCount
		imageCount += study.InstanceCount
	}
	summary := fmt.Sprintf(
		"%s patients, %s studies, %s series, %s images",
		archiveFooterCountLabel(len(patientKeys)),
		archiveFooterCountLabel(len(state.studies)),
		archiveFooterCountLabel(seriesCount),
		archiveFooterCountLabel(imageCount),
	)
	if label := activeArchiveAlbumSummaryLabel(state.selectedArchiveAlbum); label != "" {
		summary += " - Album: " + label
	}
	if scope := activeArchiveAlbumScopeSummary(state.selectedArchiveAlbum); scope != "" {
		summary += " - Scope: " + scope
	}
	if label := activeArchiveSourceSummaryLabel(state); label != "" {
		summary += " - Source: " + label
	}
	return summary
}

func archiveSeriesSummaryText(state *uiState) string {
	if state == nil || len(state.series) == 0 {
		return "0 series, 0 images"
	}
	imageCount := 0
	for _, series := range state.series {
		imageCount += series.InstanceCount
	}
	summary := fmt.Sprintf("%s series, %s images", archiveFooterCountLabel(len(state.series)), archiveFooterCountLabel(imageCount))
	if label := selectedSeriesSummaryLabel(state); label != "" {
		summary += " - Selected: " + label
	}
	return summary
}

func archiveInstancesSummaryText(state *uiState) string {
	if state == nil {
		return "0 images"
	}
	summary := fmt.Sprintf("%s images", archiveFooterCountLabel(len(state.instances)))
	if label := selectedInstanceSummaryLabel(state); label != "" {
		summary += " - Selected: " + label
	}
	return summary
}

func archiveFooterCountLabel(count int) string {
	return workstationCountCell(strconv.Itoa(count))
}

func archiveSelectedDetailsText(state *uiState) string {
	study, studyOK := selectedStudy(state)
	series, seriesOK := selectedSeries(state)
	instance, instanceOK := selectedInstance(state)
	if !studyOK && !seriesOK && !instanceOK {
		return "No archive row selected"
	}
	return strings.Join([]string{
		"Study UID: " + emptyDash(study.StudyInstanceUID),
		"Series UID: " + emptyDash(series.SeriesInstanceUID),
		"SOP Class UID: " + emptyDash(instance.SOPClassUID),
		"SOP Instance UID: " + emptyDash(instance.SOPInstanceUID),
		"Stored Path: " + emptyDash(instance.StoredPath),
	}, "\n")
}

func selectedArchiveTechnicalValue(state *uiState, field string) string {
	study, studyOK := selectedStudy(state)
	series, seriesOK := selectedSeries(state)
	instance, instanceOK := selectedInstance(state)
	switch field {
	case "Study UID":
		if !studyOK {
			return ""
		}
		return strings.TrimSpace(study.StudyInstanceUID)
	case "Series UID":
		if !seriesOK {
			return ""
		}
		return strings.TrimSpace(series.SeriesInstanceUID)
	case "SOP Class UID":
		if !instanceOK {
			return ""
		}
		return strings.TrimSpace(instance.SOPClassUID)
	case "SOP Instance UID":
		if !instanceOK {
			return ""
		}
		return strings.TrimSpace(instance.SOPInstanceUID)
	case "Stored Path":
		if !instanceOK {
			return ""
		}
		return strings.TrimSpace(instance.StoredPath)
	default:
		return ""
	}
}

func copySelectedArchiveTechnicalValue(state *uiState, status *widget.Label, field string) {
	value := selectedArchiveTechnicalValue(state, field)
	if value == "" {
		if status != nil {
			status.SetText(field + " is empty")
		}
		return
	}
	fyne.CurrentApp().Clipboard().SetContent(value)
	if status != nil {
		status.SetText("Copied " + field)
	}
}

func newArchiveSelectedDetailsPanel(state *uiState, status *widget.Label) fyne.CanvasObject {
	state.archiveSelectedDetailsLabel = compactWorkbenchLabel()
	state.archiveSelectedDetailsLabel.SetText(archiveSelectedDetailsText(state))
	copyButton := func(field string) *widget.Button {
		return widget.NewButtonWithIcon("Copy "+field, theme.ContentCopyIcon(), func() {
			copySelectedArchiveTechnicalValue(state, status, field)
		})
	}
	return container.NewBorder(
		nil,
		container.NewHBox(
			copyButton("Study UID"),
			copyButton("Series UID"),
			copyButton("SOP Class UID"),
			copyButton("SOP Instance UID"),
			copyButton("Stored Path"),
		),
		nil,
		nil,
		state.archiveSelectedDetailsLabel,
	)
}

func refreshArchiveSelectedDetails(state *uiState) {
	if state == nil || state.archiveSelectedDetailsLabel == nil {
		return
	}
	state.archiveSelectedDetailsLabel.SetText(archiveSelectedDetailsText(state))
}

func selectedSeriesSummaryLabel(state *uiState) string {
	if state == nil || state.selectedSeriesRow < 0 || state.selectedSeriesRow >= len(state.series) {
		return ""
	}
	series := state.series[state.selectedSeriesRow]
	parts := []string{}
	if value := strings.TrimSpace(series.SeriesNumber); value != "" {
		parts = append(parts, "#"+value)
	}
	if value := strings.TrimSpace(series.SeriesDescription); value != "" {
		parts = append(parts, value)
	}
	if value := strings.TrimSpace(series.Modality); value != "" {
		parts = append(parts, value)
	}
	if len(parts) == 0 {
		return strings.TrimSpace(series.SeriesInstanceUID)
	}
	return strings.Join(parts, " ")
}

func selectedInstanceSummaryLabel(state *uiState) string {
	if state == nil || state.selectedInstanceRow < 0 || state.selectedInstanceRow >= len(state.instances) {
		return ""
	}
	instance := state.instances[state.selectedInstanceRow]
	parts := []string{}
	if value := strings.TrimSpace(instance.InstanceNumber); value != "" {
		parts = append(parts, "#"+value)
	}
	if value := strings.TrimSpace(instance.SOPInstanceUID); value != "" {
		parts = append(parts, value)
	}
	return strings.Join(parts, " ")
}

func archiveAlbumLines(studies []archive.Study, now time.Time) []string {
	return []string{
		railCountLine("Database", len(studies)),
		railCountLine("Cases with comments", 0),
		railCountLine("Interesting Cases", 0),
		railCountLine("Just Acquired (last hour)", countStudiesAcquiredSince(studies, now.Add(-time.Hour))),
		railCountLine("Just Added (last hour)", countStudiesImportedSince(studies, now.Add(-time.Hour))),
		railCountLine("Just Opened", 0),
		railCountLine("Today", countStudiesImportedToday(studies, now, "")),
		railCountLine("Today CR", countStudiesImportedToday(studies, now, "CR")),
		railCountLine("Today CT", countStudiesImportedToday(studies, now, "CT")),
	}
}

func activeArchiveAlbumSummaryLabel(id archiveAlbumID) string {
	switch id {
	case "", archiveAlbumDatabase:
		return ""
	default:
		return archiveAlbumLabel(id)
	}
}

func activeArchiveAlbumScopeSummary(id archiveAlbumID) string {
	switch id {
	case archiveAlbumComments:
		return "with local comments"
	case archiveAlbumInteresting:
		return "status Interesting"
	case archiveAlbumLastHour:
		return "acquired in last hour"
	case archiveAlbumAddedLastHour:
		return "added in last hour"
	case archiveAlbumOpened:
		return "recently opened"
	case archiveAlbumToday:
		return "imported today"
	case archiveAlbumTodayCR:
		return "imported today, modality CR"
	case archiveAlbumTodayCT:
		return "imported today, modality CT"
	default:
		return ""
	}
}

func archiveAlbumScopeControlText(id archiveAlbumID) string {
	scope := activeArchiveAlbumScopeSummary(id)
	if scope == "" {
		scope = "all studies"
	}
	return "Album Scope: " + scope
}

func archiveAlbumLabel(id archiveAlbumID) string {
	switch id {
	case archiveAlbumDatabase:
		return "Database"
	case archiveAlbumComments:
		return "Cases with comments"
	case archiveAlbumInteresting:
		return "Interesting Cases"
	case archiveAlbumLastHour:
		return "Just Acquired (last hour)"
	case archiveAlbumAddedLastHour:
		return "Just Added (last hour)"
	case archiveAlbumOpened:
		return "Just Opened"
	case archiveAlbumToday:
		return "Today"
	case archiveAlbumTodayCR:
		return "Today CR"
	case archiveAlbumTodayCT:
		return "Today CT"
	default:
		return ""
	}
}

func archiveAlbumIcon(id archiveAlbumID) fyne.Resource {
	switch id {
	case archiveAlbumDatabase:
		return archiveAlbumDatabaseIconResource
	case archiveAlbumComments:
		return archiveAlbumCommentsIconResource
	case archiveAlbumInteresting:
		return archiveAlbumInterestingIconResource
	case archiveAlbumLastHour:
		return archiveAlbumAcquiredClockIconResource
	case archiveAlbumAddedLastHour:
		return archiveAlbumAddedClockIconResource
	case archiveAlbumTodayCR:
		return archiveAlbumTodayCRIconResource
	case archiveAlbumTodayCT:
		return archiveAlbumTodayCTIconResource
	case archiveAlbumOpened:
		return theme.StorageIcon()
	default:
		return theme.FolderIcon()
	}
}

func activeArchiveSourceSummaryLabel(state *uiState) string {
	if state == nil || state.selectedNodeRow < 0 || state.selectedNodeRow >= len(state.nodes) {
		return ""
	}
	return archiveNodeSourceLabel(state.nodes[state.selectedNodeRow])
}

func archiveSourceLines(state *uiState) []string {
	rows := archiveSourceRows(state)
	lines := make([]string, 0, len(rows))
	for _, row := range rows {
		prefix := "  "
		if row.Selected {
			prefix = "▶ "
		}
		lines = append(lines, prefix+row.LegacyText)
	}
	return lines
}

func archiveNodeSourceLabel(node nodes.Node) string {
	return fmt.Sprintf("%s %s:%d", node.Name, node.Host, node.Port)
}

type archiveSourceRow struct {
	Text       string
	LegacyText string
	Icon       fyne.Resource
	Selected   bool
	Selectable bool
	NodeIndex  int
}

func archiveSourceRows(state *uiState) []archiveSourceRow {
	localSelected := state == nil || state.selectedNodeRow < 0 || state.selectedNodeRow >= len(state.nodes)
	rows := []archiveSourceRow{{
		Text:       "Documents DB",
		LegacyText: "▣ Documents DB",
		Icon:       archiveSourceLocalDBIconResource,
		Selected:   localSelected,
		Selectable: true,
		NodeIndex:  -1,
	}}
	if state == nil {
		return rows
	}
	if state.receiver != nil {
		snapshot := state.receiver.Snapshot()
		rows = append(rows, archiveSourceRow{
			Text:       "Documents DB",
			LegacyText: fmt.Sprintf("● Receiver %s %s", snapshot.AETitle, snapshot.Address),
			Icon:       archiveSourceReceiverIconResource,
			NodeIndex:  -1,
		})
	} else {
		rows = append(rows, archiveSourceRow{
			Text:       "Documents DB",
			LegacyText: fmt.Sprintf("● Receiver %s stopped", localAETitle(state)),
			Icon:       archiveSourceReceiverIconResource,
			NodeIndex:  -1,
		})
	}
	for index, node := range state.nodes {
		text := strings.TrimSpace(node.Name)
		if text == "" {
			text = strings.TrimSpace(node.AETitle)
		}
		if text == "" {
			text = fmt.Sprintf("%s:%d", node.Host, node.Port)
		}
		legacyText := archiveNodeSourceLabel(node)
		rows = append(rows, archiveSourceRow{
			Text:       text,
			LegacyText: "◉ " + legacyText,
			Icon:       archiveSourceRemoteNodeIconResource,
			Selected:   index == state.selectedNodeRow,
			NodeIndex:  index,
		})
	}
	return rows
}

func archiveActivityLines(state *uiState) []string {
	rows := archiveActivityRows(state)
	lines := make([]string, 0, len(rows))
	for _, row := range rows {
		line := row.Text
		if strings.TrimSpace(row.Detail) != "" {
			line = strings.TrimSpace(line + " " + row.Detail)
		}
		lines = append(lines, line)
	}
	return lines
}

func archiveActivityRows(state *uiState) []archiveActivityRow {
	if state == nil {
		return []archiveActivityRow{{Text: "No recent activity", OperationIndex: -1}}
	}
	var rows []archiveActivityRow
	if state.activeRetrieveCancel != nil {
		rows = append(rows, archiveActivityRow{
			Text:                  "Retrieving images...",
			Detail:                retrieveProgressDetail(state.retrieveActivityLabel, state.retrieveActivityNode, state.retrieveActivityProgress),
			OperationIndex:        -1,
			Cancellable:           true,
			ProgressVisible:       retrieveProgressKnown(state.retrieveActivityProgress),
			ProgressValue:         retrieveProgressFraction(state.retrieveActivityProgress),
			IndeterminateProgress: !retrieveProgressKnown(state.retrieveActivityProgress),
		})
	}
	if strings.TrimSpace(state.activeQueryActivityLabel) != "" {
		rows = append(rows, archiveActivityRow{
			Text:            "Querying...",
			Detail:          queryActivityDetail(state),
			OperationIndex:  -1,
			ProgressVisible: queryProgressKnown(state),
			ProgressValue:   queryProgressFraction(state),
		})
	}
	if strings.TrimSpace(state.activeSendActivityLabel) != "" {
		rows = append(rows, archiveActivityRow{
			Text:            "Sending...",
			Detail:          sendActivityDetail(state),
			OperationIndex:  -1,
			ProgressVisible: sendProgressKnown(state),
			ProgressValue:   sendProgressFraction(state),
		})
	}
	if strings.TrimSpace(state.activeImportActivityLabel) != "" {
		rows = append(rows, archiveActivityRow{
			Text:                  "Importing...",
			Detail:                importActivityDetail(state),
			OperationIndex:        -1,
			IndeterminateProgress: true,
		})
	}
	for i, summary := range state.operations {
		if i >= 4 {
			break
		}
		rows = append(rows, archiveActivityRow{
			Text:           archiveActivityHistoryText(summary),
			OperationIndex: i,
			Dismissible:    true,
		})
	}
	if len(rows) == 0 {
		rows = append(rows, archiveActivityRow{Text: "No recent activity", OperationIndex: -1})
	}
	return rows
}

func archiveActivityHistoryText(summary ops.Summary) string {
	parts := []string{archiveActivityKindLabel(summary.Kind)}
	if summary.Status != "" {
		parts = append(parts, archiveActivityStatusLabel(summary.Status))
	}
	if counts := shortTaskCounts(summary.Counts); counts != "" {
		parts = append(parts, counts)
	}
	return strings.Join(parts, " ")
}

func archiveActivityStatusLabel(status ops.Status) string {
	return titleWords(string(status))
}

func archiveActivityKindLabel(kind ops.Kind) string {
	switch kind {
	case ops.KindImport:
		return "Import"
	case ops.KindQueryFind:
		return "Query"
	case ops.KindRetrieveMove:
		return "Retrieve"
	case ops.KindSendStore:
		return "Send"
	case ops.KindStorageSCP:
		return "Receive"
	default:
		return titleWords(strings.ReplaceAll(string(kind), "_", " "))
	}
}

func titleWords(text string) string {
	words := strings.Fields(text)
	for index, word := range words {
		runes := []rune(strings.ToLower(word))
		if len(runes) > 0 {
			runes[0] = []rune(strings.ToUpper(string(runes[0])))[0]
		}
		words[index] = string(runes)
	}
	return strings.Join(words, " ")
}

func queryActivityDetail(state *uiState) string {
	if state == nil {
		return "Query"
	}
	text := activityDetailSubject(state.activeQueryActivityLabel, "C-FIND")
	progress := state.activeQueryActivityProgress
	if !state.activeQueryActivityHasProgress || progress.Total <= 0 {
		return text
	}
	text += fmt.Sprintf(" %d/%d src, %d match", progress.Attempted, progress.Total, progress.Matches)
	if progress.Failures > 0 {
		text += fmt.Sprintf(", %d fail", progress.Failures)
	}
	return text
}

func sendActivityDetail(state *uiState) string {
	if state == nil {
		return "Send"
	}
	text := activityDetailSubject(state.activeSendActivityLabel, "C-STORE")
	progress := state.activeSendActivityProgress
	if !state.activeSendActivityHasProgress || progress.Total <= 0 {
		return text
	}
	text += fmt.Sprintf(" %d/%d files, sent %d", progress.Attempted, progress.Total, progress.Sent)
	if progress.Failed > 0 {
		text += fmt.Sprintf(", fail %d", progress.Failed)
	}
	if progress.Warnings > 0 {
		text += fmt.Sprintf(", warn %d", progress.Warnings)
	}
	return text
}

func activityDetailSubject(label string, dimseOperation string) string {
	label = strings.TrimSpace(label)
	dimseOperation = strings.TrimSpace(dimseOperation)
	if label == "" || dimseOperation == "" {
		return label
	}
	parts := strings.Fields(label)
	for index, part := range parts {
		if part == dimseOperation {
			return strings.Join(parts[index+1:], " ")
		}
	}
	return label
}

func importActivityDetail(state *uiState) string {
	if state == nil {
		return "Import"
	}
	text := strings.TrimSpace(state.activeImportActivityLabel)
	progress := state.activeImportActivityProgress
	if !state.activeImportActivityHasProgress || progress.ScannedFiles <= 0 {
		return text
	}
	text += fmt.Sprintf(" scan %d, store %d", progress.ScannedFiles, progress.StoredFiles)
	if progress.Duplicates > 0 {
		text += fmt.Sprintf(", dup %d", progress.Duplicates)
	}
	if progress.InvalidFiles > 0 {
		text += fmt.Sprintf(", invalid %d", progress.InvalidFiles)
	}
	return text
}

func archiveSummaryText(state *uiState) string {
	if state == nil {
		return "No study selected"
	}
	study, ok := selectedStudy(state)
	if !ok {
		return fmt.Sprintf("No study selected\n\n%d studies in archive", len(state.studies))
	}
	var b strings.Builder
	fmt.Fprintf(&b, "Patient ID: %s\n", emptyDash(study.PatientID))
	fmt.Fprintf(&b, "DOB: %s\n", emptyDash(compactDisplayDate(study.PatientBirthDate)))
	fmt.Fprintf(&b, "Study: %s\n", archiveSummaryStudyLine(study))
	if strings.TrimSpace(study.StudyDescription) != "" {
		fmt.Fprintf(&b, "%s\n", study.StudyDescription)
	}
	fmt.Fprintf(&b, "Institution: %s\n", emptyDash(study.InstitutionName))
	fmt.Fprintf(&b, "Accession: %s\n", emptyDash(study.AccessionNumber))
	fmt.Fprintf(&b, "Source: %s\n", emptyDash(archiveSummarySource(state)))
	fmt.Fprintf(&b, "Status: %s\n", emptyDash(studyCell(study, archiveStudyTableColumnStatus)))
	fmt.Fprintf(&b, "Comments: %s\n", emptyDash(studyCell(study, archiveStudyTableColumnComments)))
	fmt.Fprintf(&b, "Series: %d\n", study.SeriesCount)
	fmt.Fprintf(&b, "Images: %d\n", study.InstanceCount)
	if !study.ImportedAt.IsZero() {
		fmt.Fprintf(&b, "Added: %s\n", archiveTimestampCell(study.ImportedAt))
	}
	if series, ok := selectedSeries(state); ok {
		fmt.Fprintf(&b, "\nSelected series\n")
		fmt.Fprintf(&b, "%s %s %s\n", emptyDash(series.SeriesNumber), emptyDash(series.Modality), emptyDash(series.SeriesDescription))
		fmt.Fprintf(&b, "Series images: %d\n", series.InstanceCount)
	}
	fmt.Fprintf(&b, "\nLoaded images: %d", len(state.instances))
	return b.String()
}

func archiveSummaryStudyLine(study archive.Study) string {
	parts := []string{
		strings.TrimSpace(compactDisplayDate(study.StudyDate)),
		strings.TrimSpace(dicomTimeCell(study.StudyTime)),
		strings.TrimSpace(study.Modalities),
	}
	nonEmpty := make([]string, 0, len(parts))
	for _, part := range parts {
		if part != "" {
			nonEmpty = append(nonEmpty, part)
		}
	}
	if len(nonEmpty) == 0 {
		return "-"
	}
	return strings.Join(nonEmpty, " ")
}

func archiveSummarySource(state *uiState) string {
	if state == nil {
		return ""
	}
	for _, instance := range state.instances {
		if source := strings.TrimSpace(instance.SourcePath); source != "" {
			return source
		}
	}
	return ""
}

type patientStudySummaryRow struct {
	StudyIndex int
	Primary    string
	Modality   string
	Secondary  string
	Images     string
	Selected   bool
}

type archivePatientStudyListItem struct {
	*fyne.Container
	background    *canvas.Rectangle
	selectionIcon *widget.Icon
	primary       *widget.Label
	modality      *widget.Label
	secondary     *widget.Label
	images        *widget.Label
	metricsSlot   *fyne.Container
}

func newArchivePatientStudyListItem() *archivePatientStudyListItem {
	selectionIcon := widget.NewIcon(theme.NavigateNextIcon())
	selectionIcon.Hide()
	primary := compactWorkbenchLabel()
	primary.TextStyle = fyne.TextStyle{Bold: true}
	modality := compactWorkbenchLabel()
	modality.Alignment = fyne.TextAlignTrailing
	modality.TextStyle = fyne.TextStyle{Bold: true}
	secondary := compactWorkbenchLabel()
	images := compactWorkbenchLabel()
	images.Alignment = fyne.TextAlignTrailing
	text := container.NewVBox(primary, secondary)
	metrics := container.NewVBox(modality, images)
	metricsSlot := container.NewGridWrap(fyne.NewSize(archivePatientStudyMetricsSlotWidth, metrics.MinSize().Height), metrics)
	row := container.NewBorder(nil, nil, nil, metricsSlot, text)
	background := canvas.NewRectangle(archiveOddRowColor)
	content := container.NewStack(
		background,
		newCompactTableCellContent(row),
		newTableColumnDividerLayer(),
		newTableRowDividerLayer(),
	)
	return &archivePatientStudyListItem{
		Container:     content,
		background:    background,
		selectionIcon: selectionIcon,
		primary:       primary,
		modality:      modality,
		secondary:     secondary,
		images:        images,
		metricsSlot:   metricsSlot,
	}
}

func newArchivePatientStudyList(state *uiState, optionalTables ...archiveTables) *widget.List {
	var tables archiveTables
	if len(optionalTables) > 0 {
		tables = optionalTables[0]
	}
	list := widget.NewList(
		func() int {
			return len(patientStudySummaryRows(state, selectedArchiveStudyIndex(state)))
		},
		func() fyne.CanvasObject {
			return newArchivePatientStudyListItem()
		},
		func(id widget.ListItemID, obj fyne.CanvasObject) {
			item := obj.(*archivePatientStudyListItem)
			rows := patientStudySummaryRows(state, selectedArchiveStudyIndex(state))
			if id < 0 || id >= len(rows) {
				item.primary.SetText("")
				item.modality.SetText("")
				item.secondary.SetText("")
				item.images.SetText("")
				item.selectionIcon.Hide()
				return
			}
			row := rows[id]
			item.primary.SetText(row.Primary)
			item.modality.SetText(row.Modality)
			item.secondary.SetText(row.Secondary)
			item.images.SetText(row.Images)
			if row.Selected {
				item.background.FillColor = archiveSummarySelectedStudyRowColor
			} else {
				item.background.FillColor = archiveOddRowColor
			}
			item.selectionIcon.Hide()
			item.background.Refresh()
		},
	)
	list.HideSeparators = true
	list.OnSelected = func(id widget.ListItemID) {
		if selectArchivePatientStudyListRow(state, tables, id) && tables.studies != nil {
			tables.studies.Refresh()
		}
	}
	applyCompactArchivePatientStudyRows(list, len(patientStudySummaryRows(state, selectedArchiveStudyIndex(state))))
	return list
}

func selectArchivePatientStudyListRow(state *uiState, tables archiveTables, id widget.ListItemID) bool {
	rows := patientStudySummaryRows(state, selectedArchiveStudyIndex(state))
	if id < 0 || int(id) >= len(rows) {
		return false
	}
	row := rows[id]
	if row.StudyIndex < 0 || row.StudyIndex >= len(state.studies) {
		return false
	}
	if row.StudyIndex == state.selectedStudyRow && state.selectedSeriesRow < 0 && state.selectedInstanceRow < 0 {
		return false
	}
	state.selectedStudyRow = row.StudyIndex
	recordOpenedArchiveStudy(state, state.studies[row.StudyIndex])
	clearArchiveDetails(state, tables)
	return true
}

func selectedArchiveStudyIndex(state *uiState) int {
	if state == nil {
		return -1
	}
	return state.selectedStudyRow
}

func patientStudySummaryLines(state *uiState, selectedStudyIndex int) []string {
	rows := patientStudySummaryRows(state, selectedStudyIndex)
	lines := make([]string, 0, len(rows)*2)
	for _, row := range rows {
		prefix := "  "
		if row.Selected {
			prefix = "▶ "
		}
		lines = append(lines,
			strings.TrimSpace(fmt.Sprintf("%s%s %s", prefix, row.Primary, row.Modality)),
			strings.TrimSpace(fmt.Sprintf("%s %s", row.Secondary, row.Images)),
		)
	}
	return lines
}

func patientStudySummaryRows(state *uiState, selectedStudyIndex int) []patientStudySummaryRow {
	if state == nil || selectedStudyIndex < 0 || selectedStudyIndex >= len(state.studies) {
		return nil
	}
	selected := state.studies[selectedStudyIndex]
	selectedKey := archivePatientKey(selected)
	var rows []patientStudySummaryRow
	appendStudy := func(index int, study archive.Study, selected bool) {
		description := strings.TrimSpace(study.StudyDescription)
		if description == "" {
			description = strings.TrimSpace(study.StudyInstanceUID)
		}
		rows = append(rows, patientStudySummaryRow{
			StudyIndex: index,
			Primary:    emptyDash(description),
			Modality:   emptyDash(study.Modalities),
			Secondary:  emptyDash(compactDisplayDate(study.StudyDate)),
			Images:     archiveStudyImageCountLabel(study.InstanceCount),
			Selected:   selected,
		})
	}
	appendStudy(selectedStudyIndex, selected, true)
	for index, study := range state.studies {
		if index == selectedStudyIndex {
			continue
		}
		if archivePatientKey(study) == selectedKey {
			appendStudy(index, study, false)
		}
	}
	return rows
}

func archiveStudyImageCountLabel(count int) string {
	if count == 1 {
		return "1 image"
	}
	return strconv.Itoa(count) + " images"
}

func compactDisplayDate(value string) string {
	value = strings.TrimSpace(value)
	if len(value) != 8 {
		return value
	}
	date, err := time.ParseInLocation("20060102", value, time.Local)
	if err != nil {
		return value
	}
	return date.Format("2/1/06")
}

func compactDisplayDateTime(date string, dicomTime string) string {
	dateText := compactDisplayDate(date)
	timeText := dicomTimeCell(dicomTime)
	if dateText == "" {
		return timeText
	}
	if timeText == "" {
		return dateText
	}
	return dateText + " " + timeText
}

func displayPatientName(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return ""
	}
	parts := strings.Split(value, "^")
	nonEmpty := make([]string, 0, len(parts))
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part != "" {
			nonEmpty = append(nonEmpty, part)
		}
	}
	if len(nonEmpty) == 0 {
		return value
	}
	return strings.Join(nonEmpty, " ")
}

type archiveAlbumID string

const maxOpenedArchiveStudyUIDs = 200

const (
	archiveAlbumDatabase      archiveAlbumID = "database"
	archiveAlbumComments      archiveAlbumID = "comments"
	archiveAlbumInteresting   archiveAlbumID = "interesting"
	archiveAlbumLastHour      archiveAlbumID = "last-hour"
	archiveAlbumAddedLastHour archiveAlbumID = "added-last-hour"
	archiveAlbumOpened        archiveAlbumID = "opened"
	archiveAlbumToday         archiveAlbumID = "today"
	archiveAlbumTodayCR       archiveAlbumID = "today-cr"
	archiveAlbumTodayCT       archiveAlbumID = "today-ct"
)

type archiveAlbumRow struct {
	ID         archiveAlbumID
	Label      string
	Count      int
	Text       string
	Selected   bool
	Filterable bool
}

func railCountLine(label string, count int) string {
	return fmt.Sprintf("%-33s%d", label, count)
}

func archiveAlbumRailLabel(id archiveAlbumID) string {
	switch id {
	case archiveAlbumLastHour:
		return "Just Acqu...(last hour)"
	case archiveAlbumAddedLastHour:
		return "Just Adde...(last hour)"
	default:
		return archiveAlbumLabel(id)
	}
}

func archiveAlbumRows(studies []archive.Study, now time.Time, selected archiveAlbumID) []archiveAlbumRow {
	return archiveAlbumRowsWithOpened(studies, now, selected, nil)
}

func archiveAlbumRowsForState(state *uiState, now time.Time) []archiveAlbumRow {
	if state == nil {
		return archiveAlbumRowsWithOpened(nil, now, "", nil)
	}
	return archiveAlbumRowsWithOpened(state.studies, now, state.selectedArchiveAlbum, state.openedArchiveStudyUIDs)
}

func archiveAlbumRowsWithOpened(studies []archive.Study, now time.Time, selected archiveAlbumID, openedUIDs map[string]bool) []archiveAlbumRow {
	rows := []archiveAlbumRow{
		{ID: archiveAlbumDatabase, Label: archiveAlbumLabel(archiveAlbumDatabase), Count: len(studies), Filterable: true},
		{ID: archiveAlbumComments, Label: archiveAlbumLabel(archiveAlbumComments), Count: countStudiesWithComments(studies), Filterable: true},
		{ID: archiveAlbumInteresting, Label: archiveAlbumLabel(archiveAlbumInteresting), Count: countStudiesWithStatus(studies, studyStatusPresetInterestingLabel), Filterable: true},
		{ID: archiveAlbumLastHour, Label: archiveAlbumLabel(archiveAlbumLastHour), Count: countStudiesAcquiredSince(studies, now.Add(-time.Hour)), Filterable: true},
		{ID: archiveAlbumAddedLastHour, Label: archiveAlbumLabel(archiveAlbumAddedLastHour), Count: countStudiesImportedSince(studies, now.Add(-time.Hour)), Filterable: true},
		{ID: archiveAlbumOpened, Label: archiveAlbumLabel(archiveAlbumOpened), Count: countStudiesByOpenedUIDs(studies, openedUIDs), Filterable: true},
		{ID: archiveAlbumTodayCR, Label: archiveAlbumLabel(archiveAlbumTodayCR), Count: countStudiesImportedToday(studies, now, "CR"), Filterable: true},
		{ID: archiveAlbumTodayCT, Label: archiveAlbumLabel(archiveAlbumTodayCT), Count: countStudiesImportedToday(studies, now, "CT"), Filterable: true},
	}
	for i := range rows {
		if selected != "" && rows[i].ID == selected {
			rows[i].Selected = true
		}
		rows[i].Text = "  " + railCountLine(archiveAlbumRailLabel(rows[i].ID), rows[i].Count)
	}
	return rows
}

func archiveAlbumFilters(id archiveAlbumID, now time.Time) (archive.StudyFilters, bool) {
	switch id {
	case archiveAlbumDatabase:
		return archive.StudyFilters{}, true
	case archiveAlbumComments:
		return archive.StudyFilters{HasComments: true}, true
	case archiveAlbumInteresting:
		return archive.StudyFilters{Status: studyStatusPresetInterestingLabel}, true
	case archiveAlbumLastHour:
		return archive.StudyFilters{
			StudyDateTimeFrom: studyDateTimeFilterBound(now.Add(-time.Hour)),
			StudyDateTimeTo:   studyDateTimeFilterBound(now),
		}, true
	case archiveAlbumAddedLastHour:
		return archive.StudyFilters{
			ImportedAtFrom: now.Add(-time.Hour).UTC().Format(time.RFC3339Nano),
		}, true
	case archiveAlbumOpened:
		return archive.StudyFilters{}, true
	case archiveAlbumToday, archiveAlbumTodayCR, archiveAlbumTodayCT:
		start, end := localDayBounds(now)
		filters := archive.StudyFilters{
			ImportedAtFrom: start.UTC().Format(time.RFC3339Nano),
			ImportedAtTo:   end.UTC().Format(time.RFC3339Nano),
		}
		if id == archiveAlbumTodayCR {
			filters.Modalities = []string{"CR"}
		}
		if id == archiveAlbumTodayCT {
			filters.Modalities = []string{"CT"}
		}
		return filters, true
	default:
		return archive.StudyFilters{}, false
	}
}

func archiveFiltersWithAlbum(base archive.StudyFilters, id archiveAlbumID, now time.Time) (archive.StudyFilters, bool) {
	albumFilters, ok := archiveAlbumFilters(id, now)
	if !ok {
		return archive.StudyFilters{}, false
	}
	base.ImportedAtFrom = albumFilters.ImportedAtFrom
	base.ImportedAtTo = albumFilters.ImportedAtTo
	base.StudyDateTimeFrom = albumFilters.StudyDateTimeFrom
	base.StudyDateTimeTo = albumFilters.StudyDateTimeTo
	base.Modalities = albumFilters.Modalities
	base.Status = albumFilters.Status
	base.HasComments = albumFilters.HasComments
	return base, true
}

func localDayBounds(now time.Time) (time.Time, time.Time) {
	year, month, day := now.Local().Date()
	location := now.Local().Location()
	start := time.Date(year, month, day, 0, 0, 0, 0, location)
	return start, start.AddDate(0, 0, 1).Add(-time.Nanosecond)
}

func countStudiesImportedSince(studies []archive.Study, since time.Time) int {
	count := 0
	for _, study := range studies {
		if !study.ImportedAt.IsZero() && !study.ImportedAt.Before(since) {
			count++
		}
	}
	return count
}

func countStudiesAcquiredSince(studies []archive.Study, since time.Time) int {
	count := 0
	for _, study := range studies {
		acquiredAt, ok := studyAcquiredAt(study, since.Location())
		if ok && !acquiredAt.Before(since) {
			count++
		}
	}
	return count
}

func studyAcquiredAt(study archive.Study, location *time.Location) (time.Time, bool) {
	if location == nil {
		location = time.Local
	}
	date := strings.TrimSpace(study.StudyDate)
	tm := normalizedDICOMTimeForDateTime(study.StudyTime)
	if len(date) != 8 || tm == "" {
		return time.Time{}, false
	}
	value, err := time.ParseInLocation("20060102150405", date+tm, location)
	if err != nil {
		return time.Time{}, false
	}
	return value, true
}

func normalizedDICOMTimeForDateTime(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return ""
	}
	value = strings.SplitN(value, ".", 2)[0]
	if len(value) > 6 {
		value = value[:6]
	}
	for len(value) < 6 {
		value += "0"
	}
	return value
}

func studyDateTimeFilterBound(value time.Time) string {
	return value.Local().Format("20060102150405")
}

func countStudiesByOpenedUIDs(studies []archive.Study, openedUIDs map[string]bool) int {
	return len(filterStudiesByOpenedUIDs(studies, openedUIDs))
}

func filterStudiesByOpenedUIDs(studies []archive.Study, openedUIDs map[string]bool) []archive.Study {
	if len(studies) == 0 || len(openedUIDs) == 0 {
		return nil
	}
	filtered := make([]archive.Study, 0, len(studies))
	for _, study := range studies {
		if openedUIDs[strings.TrimSpace(study.StudyInstanceUID)] {
			filtered = append(filtered, study)
		}
	}
	return filtered
}

func countStudiesWithComments(studies []archive.Study) int {
	count := 0
	for _, study := range studies {
		if strings.TrimSpace(study.Comments) != "" {
			count++
		}
	}
	return count
}

func countStudiesWithStatus(studies []archive.Study, status string) int {
	status = strings.ToLower(strings.TrimSpace(status))
	if status == "" {
		return 0
	}
	count := 0
	for _, study := range studies {
		if strings.ToLower(strings.TrimSpace(study.Status)) == status {
			count++
		}
	}
	return count
}

func countStudiesImportedToday(studies []archive.Study, now time.Time, modality string) int {
	count := 0
	for _, study := range studies {
		if !sameLocalDate(study.ImportedAt, now) {
			continue
		}
		if modality != "" && !hasModality(study.Modalities, modality) {
			continue
		}
		count++
	}
	return count
}

func sameLocalDate(a, b time.Time) bool {
	if a.IsZero() || b.IsZero() {
		return false
	}
	ay, am, ad := a.Local().Date()
	by, bm, bd := b.Local().Date()
	return ay == by && am == bm && ad == bd
}

func hasModality(modalities string, modality string) bool {
	modality = strings.ToUpper(strings.TrimSpace(modality))
	for _, part := range strings.FieldsFunc(modalities, func(r rune) bool {
		return r == ',' || r == '\\' || r == '/' || r == ';' || r == ' '
	}) {
		if strings.ToUpper(strings.TrimSpace(part)) == modality {
			return true
		}
	}
	return false
}

func shortTaskCounts(counts ops.Counts) string {
	var parts []string
	appendCount := func(label string, value *uint64) {
		if value != nil && *value > 0 {
			parts = append(parts, fmt.Sprintf("%s %d", label, *value))
		}
	}
	appendCount("match", counts.Matched)
	appendCount("store", counts.Stored)
	appendCount("recv", counts.Received)
	appendCount("sent", counts.Sent)
	appendCount("fail", counts.Failed)
	if len(parts) == 0 {
		return ""
	}
	return strings.Join(parts, ", ")
}

func importFileDialog(w fyne.Window, status *widget.Label, tables archiveTables, state *uiState) {
	picker := dialog.NewFileOpen(func(reader fyne.URIReadCloser, err error) {
		if err != nil {
			dialog.ShowError(err, w)
			return
		}
		if reader == nil {
			return
		}
		path := reader.URI().Path()
		_ = reader.Close()
		importPathAsync(w, status, tables, state, path)
	}, w)
	picker.Show()
}

func importFolderDialog(w fyne.Window, status *widget.Label, tables archiveTables, state *uiState) {
	picker := dialog.NewFolderOpen(func(uri fyne.ListableURI, err error) {
		if err != nil {
			dialog.ShowError(err, w)
			return
		}
		if uri == nil {
			return
		}
		importPathAsync(w, status, tables, state, uri.Path())
	}, w)
	picker.Show()
}

func importPathAsync(w fyne.Window, status *widget.Label, tables archiveTables, state *uiState, path string) {
	if path == "" {
		return
	}
	pathLabel := filepath.Base(path)
	status.SetText("Importing " + pathLabel)
	beginImportActivity(state, pathLabel)
	started := time.Now()
	go func() {
		opts := importOptionsFromConfig(state.appConfig)
		opts.OnProgress = importProgressCallback(state)
		report, err := state.catalog.ImportPathWithOptions(context.Background(), path, opts)
		summary := ops.ImportSummary(report, time.Since(started))
		studies, studyErr := loadStudies(context.Background(), state)
		fyne.Do(func() {
			clearActiveImportActivity(state)
			if err != nil {
				status.SetText("Import failed")
				dialog.ShowError(err, w)
				return
			}
			recordOperation(state, summary)
			if studyErr != nil {
				status.SetText("Import completed, refresh failed")
				dialog.ShowError(studyErr, w)
				return
			}
			setStudies(state, tables, studies)
			status.SetText(fmt.Sprintf(
				"Scanned %d, stored %d, duplicates %d, invalid %d",
				report.ScannedFiles,
				report.StoredFiles,
				report.Duplicates,
				report.InvalidFiles,
			))
		})
	}()
}

type archiveControlSet struct {
	toolbarSearch   fyne.CanvasObject
	archiveControls fyne.CanvasObject
}

func newArchiveControls(w fyne.Window, status *widget.Label, tables archiveTables, state *uiState) fyne.CanvasObject {
	return newArchiveControlSet(w, status, tables, state).archiveControls
}

func newArchiveControlSet(w fyne.Window, status *widget.Label, tables archiveTables, state *uiState) archiveControlSet {
	quickSearchField := widget.NewSelect(archiveQuickSearchOptions, nil)
	quickSearchField.SetSelected(archiveQuickSearchPatientName)
	quickSearch := widget.NewEntry()
	quickSearch.SetPlaceHolder(archiveQuickSearchPlaceholder(quickSearchField.Selected))
	patientName := widget.NewEntry()
	patientName.SetPlaceHolder("Patient")
	patientID := widget.NewEntry()
	patientID.SetPlaceHolder("Patient ID")
	accession := widget.NewEntry()
	accession.SetPlaceHolder("Accession")
	studyDescription := widget.NewEntry()
	studyDescription.SetPlaceHolder("Description")
	modality := widget.NewEntry()
	modality.SetPlaceHolder("CT,MR")
	studyDateFrom := widget.NewEntry()
	studyDateFrom.SetPlaceHolder("20260101")
	studyDateTo := widget.NewEntry()
	studyDateTo.SetPlaceHolder("20261231")
	importedAtFrom := widget.NewEntry()
	importedAtFrom.SetPlaceHolder("2026-06-04T00:00:00Z")
	importedAtTo := widget.NewEntry()
	importedAtTo.SetPlaceHolder("2026-06-04T23:59:59Z")
	sourcePath := widget.NewEntry()
	sourcePath.SetPlaceHolder("source path")
	seriesModality := widget.NewEntry()
	seriesModality.SetPlaceHolder("CT")
	seriesNumber := widget.NewEntry()
	seriesNumber.SetPlaceHolder("1")
	seriesDescription := widget.NewEntry()
	seriesDescription.SetPlaceHolder("Axial")

	applyingProgrammaticSearchText := false
	applyAdvancedFilters := func() {
		studyFilters := archive.StudyFilters{
			PatientName:      patientName.Text,
			PatientID:        patientID.Text,
			AccessionNumber:  accession.Text,
			StudyDescription: studyDescription.Text,
			StudyDateFrom:    studyDateFrom.Text,
			StudyDateTo:      studyDateTo.Text,
			ImportedAtFrom:   importedAtFrom.Text,
			ImportedAtTo:     importedAtTo.Text,
			Modalities:       splitModalities(modality.Text),
			SourcePath:       sourcePath.Text,
		}
		if state.selectedArchiveAlbum != "" && state.selectedArchiveAlbum != archiveAlbumDatabase {
			if albumFilters, ok := archiveFiltersWithAlbum(studyFilters, state.selectedArchiveAlbum, time.Now()); ok {
				studyFilters = albumFilters
			}
		}
		state.studyFilters = studyFilters
		state.seriesFilters = archive.SeriesFilters{
			Modality:          seriesModality.Text,
			SeriesNumber:      seriesNumber.Text,
			SeriesDescription: seriesDescription.Text,
		}
		applyingProgrammaticSearchText = true
		switch quickSearchField.Selected {
		case archiveQuickSearchPatientID:
			quickSearch.SetText(strings.TrimSpace(patientID.Text))
		case archiveQuickSearchAccession:
			quickSearch.SetText(strings.TrimSpace(accession.Text))
		default:
			quickSearch.SetText(strings.TrimSpace(patientName.Text))
		}
		applyingProgrammaticSearchText = false
		refreshStudies(w, status, tables, state)
	}
	applyButton := widget.NewButtonWithIcon("Apply Filters", theme.SearchIcon(), applyAdvancedFilters)
	soundexCheck := widget.NewCheck("Soundex", nil)
	searchModeLabel := compactWorkbenchLabel()
	searchModeLabel.SetText(archiveQuickSearchModeText(quickSearchField.Selected))
	applyQuickSearch := func() {
		filters, ok := archiveFiltersWithQuickSearchFieldAndSoundex(state.studyFilters, quickSearchField.Selected, quickSearch.Text, soundexCheck.Checked)
		if !ok {
			status.SetText("Search failed")
			if w != nil {
				dialog.ShowError(fmt.Errorf("unsupported archive search field %q", quickSearchField.Selected), w)
			}
			return
		}
		state.studyFilters = filters
		patientName.SetText(state.studyFilters.PatientName)
		patientID.SetText(state.studyFilters.PatientID)
		accession.SetText(state.studyFilters.AccessionNumber)
		refreshStudies(w, status, tables, state)
	}
	quickSearchField.OnChanged = func(field string) {
		searchModeLabel.SetText(archiveQuickSearchModeText(field))
		quickSearch.SetPlaceHolder(archiveQuickSearchPlaceholder(field))
		if strings.TrimSpace(quickSearch.Text) != "" {
			applyQuickSearch()
		}
	}
	quickSearch.OnChanged = func(_ string) {
		if applyingProgrammaticSearchText {
			return
		}
		applyQuickSearch()
	}
	quickSearch.OnSubmitted = func(_ string) {
		applyQuickSearch()
	}
	soundexCheck.OnChanged = func(_ bool) {
		if quickSearchField.Selected == archiveQuickSearchPatientName && strings.TrimSpace(quickSearch.Text) != "" {
			applyQuickSearch()
		}
	}
	clearButton := widget.NewButtonWithIcon("Clear", theme.ContentClearIcon(), func() {
		applyingProgrammaticSearchText = true
		quickSearch.SetText("")
		quickSearchField.SetSelected(archiveQuickSearchPatientName)
		applyingProgrammaticSearchText = false
		patientName.SetText("")
		patientID.SetText("")
		accession.SetText("")
		studyDescription.SetText("")
		modality.SetText("")
		studyDateFrom.SetText("")
		studyDateTo.SetText("")
		importedAtFrom.SetText("")
		importedAtTo.SetText("")
		sourcePath.SetText("")
		seriesModality.SetText("")
		seriesNumber.SetText("")
		seriesDescription.SetText("")
		state.studyFilters = archive.StudyFilters{}
		state.seriesFilters = archive.SeriesFilters{}
		state.selectedArchiveAlbum = archiveAlbumDatabase
		refreshStudies(w, status, tables, state)
	})
	exportButton := widget.NewButtonWithIcon("Export CSV", theme.DocumentSaveIcon(), func() {
		exportStudiesCSV(w, status, state)
	})
	exportJSONButton := widget.NewButtonWithIcon("Export JSON", theme.DocumentSaveIcon(), func() {
		exportStudiesJSON(w, status, state)
	})
	exportSeriesCSVButton := widget.NewButtonWithIcon("Export Series CSV", theme.DocumentSaveIcon(), func() {
		exportSeriesCSV(w, status, state)
	})
	exportSeriesJSONButton := widget.NewButtonWithIcon("Export Series JSON", theme.DocumentSaveIcon(), func() {
		exportSeriesJSON(w, status, state)
	})
	exportImagesCSVButton := widget.NewButtonWithIcon("Export Images CSV", theme.DocumentSaveIcon(), func() {
		exportImagesCSV(w, status, state)
	})
	exportImagesJSONButton := widget.NewButtonWithIcon("Export Images JSON", theme.DocumentSaveIcon(), func() {
		exportImagesJSON(w, status, state)
	})

	showQuickSearchFieldMenu := func(anchor fyne.CanvasObject) {
		if anchor == nil || fyne.CurrentApp() == nil {
			return
		}
		menuCanvas := fyne.CurrentApp().Driver().CanvasForObject(anchor)
		if menuCanvas == nil {
			return
		}
		menu := widget.NewPopUpMenu(newArchiveQuickSearchFieldMenu(quickSearchField.Selected, func(field string) {
			quickSearchField.SetSelected(field)
		}), menuCanvas)
		menu.ShowAtRelativePosition(fyne.NewPos(0, anchor.MinSize().Height), anchor)
	}
	quickSearchBox := newArchiveToolbarQuickSearchBox(quickSearch, soundexCheck, searchModeLabel, showQuickSearchFieldMenu)
	quickRow := workbenchStrip(quickSearchBox)
	albumScope := compactWorkbenchLabel()
	albumScope.SetText(archiveAlbumScopeControlText(state.selectedArchiveAlbum))
	state.archiveAlbumScopeLabel = albumScope
	lastSyncedAlbum := archiveAlbumID("__unsynced__")
	state.archiveAdvancedFilterSync = func() {
		if lastSyncedAlbum == state.selectedArchiveAlbum {
			return
		}
		lastSyncedAlbum = state.selectedArchiveAlbum
		patientName.SetText(state.studyFilters.PatientName)
		patientID.SetText(state.studyFilters.PatientID)
		accession.SetText(state.studyFilters.AccessionNumber)
		studyDescription.SetText(state.studyFilters.StudyDescription)
		modality.SetText(strings.Join(state.studyFilters.Modalities, ","))
		studyDateFrom.SetText(state.studyFilters.StudyDateFrom)
		studyDateTo.SetText(state.studyFilters.StudyDateTo)
		importedAtFrom.SetText(state.studyFilters.ImportedAtFrom)
		importedAtTo.SetText(state.studyFilters.ImportedAtTo)
		sourcePath.SetText(state.studyFilters.SourcePath)
		seriesModality.SetText(state.seriesFilters.Modality)
		seriesNumber.SetText(state.seriesFilters.SeriesNumber)
		seriesDescription.SetText(state.seriesFilters.SeriesDescription)
		albumScope.SetText(archiveAlbumScopeControlText(state.selectedArchiveAlbum))
	}
	state.archiveAdvancedFilterSync()
	filters := container.NewVBox(
		workbenchStrip(albumScope),
		container.NewGridWithColumns(3,
			labeledEntry("Patient", patientName),
			labeledEntry("Patient ID", patientID),
			labeledEntry("Modality", modality),
		),
		container.NewGridWithColumns(3,
			labeledEntry("Study Date From", studyDateFrom),
			labeledEntry("Study Date To", studyDateTo),
			labeledEntry("Source", sourcePath),
		),
		container.NewGridWithColumns(4,
			labeledEntry("Accession", accession),
			labeledEntry("Description", studyDescription),
			labeledEntry("Imported From", importedAtFrom),
			labeledEntry("Imported To", importedAtTo),
		),
		container.NewGridWithColumns(3,
			labeledEntry("Series Modality", seriesModality),
			labeledEntry("Series #", seriesNumber),
			labeledEntry("Series Description", seriesDescription),
		),
		container.NewHBox(layout.NewSpacer(), applyButton, clearButton),
	)
	advancedFilters := widget.NewAccordion(widget.NewAccordionItem("Advanced Filters", filters))
	exportActions := container.NewHScroll(container.NewHBox(exportButton, exportJSONButton, exportSeriesCSVButton, exportSeriesJSONButton, exportImagesCSVButton, exportImagesJSONButton))
	return archiveControlSet{
		toolbarSearch:   quickRow,
		archiveControls: container.NewVBox(advancedFilters, exportActions),
	}
}

func newArchiveToolbarQuickSearchBox(entry *widget.Entry, soundex *widget.Check, modeLabel *widget.Label, fieldMenuTapped ...func(fyne.CanvasObject)) fyne.CanvasObject {
	modeLabel.Alignment = fyne.TextAlignTrailing
	modeLabel.TextStyle = fyne.TextStyle{}
	modeLabel.Wrapping = fyne.TextTruncate
	var fieldMenuButton *widget.Button
	fieldMenuButton = widget.NewButtonWithIcon("", theme.MenuDropDownIcon(), func() {
		if len(fieldMenuTapped) > 0 && fieldMenuTapped[0] != nil {
			fieldMenuTapped[0](fieldMenuButton)
		}
	})
	fieldMenuButton.Importance = widget.LowImportance
	if len(fieldMenuTapped) == 0 || fieldMenuTapped[0] == nil {
		fieldMenuButton.Disable()
	}
	entryRow := container.NewBorder(nil, nil, widget.NewIcon(theme.SearchIcon()), fieldMenuButton, entry)
	modeRow := container.NewBorder(nil, nil, soundex, nil, modeLabel)
	return container.NewVBox(
		container.NewGridWrap(fyne.NewSize(archiveToolbarQuickSearchWidth, entry.MinSize().Height), entryRow),
		container.NewGridWrap(fyne.NewSize(archiveToolbarQuickSearchWidth, modeRow.MinSize().Height), modeRow),
	)
}

func newArchiveQuickSearchFieldMenu(selected string, choose func(string)) *fyne.Menu {
	return fyne.NewMenu("Search Field", archiveQuickSearchFieldMenuItems(selected, choose)...)
}

func archiveQuickSearchFieldMenuItems(selected string, choose func(string)) []*fyne.MenuItem {
	items := make([]*fyne.MenuItem, 0, len(archiveQuickSearchOptions))
	for _, option := range archiveQuickSearchOptions {
		field := option
		item := fyne.NewMenuItem(archiveQuickSearchPlaceholder(field), func() {
			if choose != nil {
				choose(field)
			}
		})
		item.Checked = field == selected
		items = append(items, item)
	}
	return items
}

func labeledEntry(label string, entry *widget.Entry) fyne.CanvasObject {
	return labeledControl(label, entry)
}

func labeledControl(label string, control fyne.CanvasObject) fyne.CanvasObject {
	text := widget.NewLabelWithStyle(label, fyne.TextAlignLeading, fyne.TextStyle{Bold: true})
	text.Wrapping = fyne.TextTruncate
	return container.NewBorder(nil, nil, text, nil, control)
}

type disableableControl interface {
	Disable()
	Enable()
}

func setDisableableControl(control disableableControl, disabled bool) {
	if control == nil {
		return
	}
	if disabled {
		control.Disable()
		return
	}
	control.Enable()
}

func archiveFiltersWithQuickSearch(filters archive.StudyFilters, query string) archive.StudyFilters {
	filters, _ = archiveFiltersWithQuickSearchField(filters, archiveQuickSearchPatientName, query)
	return filters
}

func archiveFiltersWithQuickSearchField(filters archive.StudyFilters, field string, query string) (archive.StudyFilters, bool) {
	return archiveFiltersWithQuickSearchFieldAndSoundex(filters, field, query, false)
}

func archiveFiltersWithQuickSearchFieldAndSoundex(filters archive.StudyFilters, field string, query string, soundex bool) (archive.StudyFilters, bool) {
	query = strings.TrimSpace(query)
	filters.PatientNameSoundex = false
	switch field {
	case archiveQuickSearchPatientName:
		filters.PatientName = query
		filters.PatientNameSoundex = query != "" && soundex
	case archiveQuickSearchPatientID:
		filters.PatientID = query
	case archiveQuickSearchAccession:
		filters.AccessionNumber = query
	default:
		return archive.StudyFilters{}, false
	}
	return filters, true
}

func queryDatePresetRange(preset string, now time.Time) (string, string, bool) {
	from, to, _, _, ok := queryDateTimePresetRange(preset, now)
	return from, to, ok
}

func queryDatePresetColumns() [][]string {
	columns := [][]string{
		{
			queryDatePresetAny,
			queryDatePresetTodayAM,
			queryDatePresetTodayPM,
			queryDatePresetToday,
			queryDatePresetYesterday,
			queryDatePresetDayBeforeYesterday,
			queryDatePresetLast2Days,
			queryDatePresetLast7Days,
		},
		{
			queryDatePresetLastMonth,
			queryDatePresetLast3Months,
			queryDatePresetOn,
			queryDatePresetBetween,
		},
		{
			queryDatePresetLast30Min,
			queryDatePresetLast1Hour,
			queryDatePresetLast2Hours,
			queryDatePresetLast3Hours,
			queryDatePresetLast6Hours,
			queryDatePresetLast8Hours,
			queryDatePresetLast12Hours,
			queryDatePresetLast24Hours,
			queryDatePresetLastNHours,
		},
	}
	out := make([][]string, len(columns))
	for i, column := range columns {
		out[i] = append([]string(nil), column...)
	}
	return out
}

func flattenQueryDatePresetColumns(columns [][]string) []string {
	var options []string
	for _, column := range columns {
		options = append(options, column...)
	}
	return options
}

func normalizeQueryDatePreset(preset string) string {
	switch strings.TrimSpace(preset) {
	case "On":
		return queryDatePresetOn
	case "Between":
		return queryDatePresetBetween
	default:
		return strings.TrimSpace(preset)
	}
}

func queryDatePresetPreservesManualRange(preset string) bool {
	preset = normalizeQueryDatePreset(preset)
	return preset == queryDatePresetBetween
}

func queryDateTimePresetRange(preset string, now time.Time) (string, string, string, string, bool) {
	return queryDateTimePresetRangeWithInputs(preset, "", "", now)
}

func queryDateTimePresetRangeWithLastHours(preset string, hours string, now time.Time) (string, string, string, string, bool) {
	return queryDateTimePresetRangeWithInputs(preset, "", hours, now)
}

func queryDateTimePresetRangeWithInputs(preset string, onDate string, hours string, now time.Time) (string, string, string, string, bool) {
	preset = normalizeQueryDatePreset(preset)
	formatDay := func(value time.Time) string {
		return value.Local().Format("20060102")
	}
	formatTime := func(value time.Time) string {
		return value.Local().Format("150405")
	}
	lastDurationRange := func(duration time.Duration) (string, string, string, string, bool) {
		if duration <= 0 {
			return "", "", "", "", false
		}
		from := now.Add(-duration)
		return formatDay(from), formatDay(now), formatTime(from), formatTime(now), true
	}
	switch preset {
	case queryDatePresetAny:
		return "", "", "", "", true
	case queryDatePresetOn:
		onDate = strings.TrimSpace(onDate)
		if onDate == "" {
			return "", "", "", "", false
		}
		return onDate, onDate, "", "", true
	case queryDatePresetTodayAM:
		day := formatDay(now)
		return day, day, "000000", "115959", true
	case queryDatePresetTodayPM:
		day := formatDay(now)
		return day, day, "120000", "235959", true
	case queryDatePresetToday:
		day := formatDay(now)
		return day, day, "", "", true
	case queryDatePresetYesterday:
		day := formatDay(now.AddDate(0, 0, -1))
		return day, day, "", "", true
	case queryDatePresetDayBeforeYesterday:
		day := formatDay(now.AddDate(0, 0, -2))
		return day, day, "", "", true
	case queryDatePresetLast2Days:
		return formatDay(now.AddDate(0, 0, -1)), formatDay(now), "", "", true
	case queryDatePresetLast7Days:
		return formatDay(now.AddDate(0, 0, -6)), formatDay(now), "", "", true
	case queryDatePresetLastMonth:
		return formatDay(now.AddDate(0, -1, 1)), formatDay(now), "", "", true
	case queryDatePresetLast3Months:
		return formatDay(now.AddDate(0, -3, 1)), formatDay(now), "", "", true
	case queryDatePresetLast30Min:
		return lastDurationRange(30 * time.Minute)
	case queryDatePresetLast1Hour:
		return lastDurationRange(time.Hour)
	case queryDatePresetLast2Hours:
		return lastDurationRange(2 * time.Hour)
	case queryDatePresetLast3Hours:
		return lastDurationRange(3 * time.Hour)
	case queryDatePresetLast6Hours:
		return lastDurationRange(6 * time.Hour)
	case queryDatePresetLast8Hours:
		return lastDurationRange(8 * time.Hour)
	case queryDatePresetLast12Hours:
		return lastDurationRange(12 * time.Hour)
	case queryDatePresetLast24Hours:
		return lastDurationRange(24 * time.Hour)
	case queryDatePresetLastNHours:
		count, err := strconv.Atoi(strings.TrimSpace(hours))
		if err != nil || count <= 0 {
			return "", "", "", "", false
		}
		return lastDurationRange(time.Duration(count) * time.Hour)
	default:
		return "", "", "", "", false
	}
}

func stepDICOMDate(value string, deltaDays int) (string, bool) {
	value = strings.TrimSpace(value)
	if len(value) != 8 {
		return "", false
	}
	for _, r := range value {
		if r < '0' || r > '9' {
			return "", false
		}
	}
	date, err := time.ParseInLocation("20060102", value, time.Local)
	if err != nil {
		return "", false
	}
	return date.AddDate(0, 0, deltaDays).Format("20060102"), true
}

func stepPositiveInteger(value string, delta int, min int) string {
	if min < 1 {
		min = 1
	}
	current, err := strconv.Atoi(strings.TrimSpace(value))
	if err != nil || current < min {
		return strconv.Itoa(min)
	}
	current += delta
	if current < min {
		current = min
	}
	return strconv.Itoa(current)
}

func queryCriteriaWithQuickSearch(criteria query.Criteria, field string, value string) (query.Criteria, bool) {
	value = strings.TrimSpace(value)
	if value == "" {
		return criteria, true
	}
	switch field {
	case queryQuickSearchPatientName:
		criteria.PatientName = value
	case queryQuickSearchPatientID:
		criteria.PatientID = value
	case queryQuickSearchAccession:
		criteria.AccessionNumber = value
	case queryQuickSearchBirthdate:
		criteria.PatientBirthDate = value
	case queryQuickSearchDescription:
		criteria.StudyDescription = value
	case queryQuickSearchReferringPhysician:
		criteria.ReferringPhysicianName = value
	case queryQuickSearchInstitution:
		criteria.InstitutionName = value
	case queryQuickSearchComments:
		criteria.PatientComments = value
	case queryQuickSearchCustomDICOMField:
		keyword, fieldValue, ok := parseCustomDICOMSearch(value)
		if !ok {
			return query.Criteria{}, false
		}
		criteria.CustomFieldKeyword = keyword
		criteria.CustomFieldValue = fieldValue
	case queryQuickSearchStatus:
		criteria.StudyStatusID = value
	default:
		return query.Criteria{}, false
	}
	return criteria, true
}

func parseCustomDICOMSearch(value string) (string, string, bool) {
	keyword, fieldValue, ok := strings.Cut(value, "=")
	if !ok {
		return "", "", false
	}
	keyword = strings.TrimSpace(keyword)
	fieldValue = strings.TrimSpace(fieldValue)
	if keyword == "" || fieldValue == "" {
		return "", "", false
	}
	return keyword, fieldValue, true
}

func queryPatientCriteriaWithQuickSearch(criteria query.PatientCriteria, field string, value string) (query.PatientCriteria, bool) {
	value = strings.TrimSpace(value)
	if value == "" {
		return criteria, true
	}
	switch field {
	case queryQuickSearchPatientName:
		criteria.PatientName = value
	case queryQuickSearchPatientID:
		criteria.PatientID = value
	case queryQuickSearchBirthdate:
		criteria.PatientBirthDate = value
	case queryQuickSearchComments:
		criteria.PatientComments = value
	default:
		return query.PatientCriteria{}, false
	}
	return criteria, true
}

func querySeriesCriteriaWithQuickSearch(criteria query.SeriesCriteria, field string, value string) (query.SeriesCriteria, bool) {
	value = strings.TrimSpace(value)
	if value == "" {
		return criteria, true
	}
	switch field {
	case queryQuickSearchPatientName:
		criteria.PatientName = value
	case queryQuickSearchPatientID:
		criteria.PatientID = value
	case queryQuickSearchDescription:
		criteria.SeriesDescription = value
	default:
		return query.SeriesCriteria{}, false
	}
	return criteria, true
}

func queryImageCriteriaWithQuickSearch(criteria query.ImageCriteria, field string, value string) (query.ImageCriteria, bool) {
	value = strings.TrimSpace(value)
	if value == "" {
		return criteria, true
	}
	switch field {
	case queryQuickSearchPatientName:
		criteria.PatientName = value
	case queryQuickSearchPatientID:
		criteria.PatientID = value
	default:
		return query.ImageCriteria{}, false
	}
	return criteria, true
}

func queryModalityCriteriaText(manual string, checks map[string]*widget.Check) string {
	selected := selectedQueryModalities(checks)
	if len(selected) > 0 {
		return strings.Join(selected, "\\")
	}
	return strings.TrimSpace(manual)
}

func newQueryModalityChecks() map[string]*widget.Check {
	checks := make(map[string]*widget.Check, len(queryModalityCodes))
	for _, code := range queryModalityCodes {
		checks[code] = widget.NewCheck(code, nil)
	}
	return checks
}

func queryModalityColumns() [][]string {
	columns := [][]string{
		queryModalityCodes[:10],
		queryModalityCodes[10:],
	}
	out := make([][]string, len(columns))
	for i, column := range columns {
		out[i] = append([]string(nil), column...)
	}
	return out
}

func queryModalityGrid(checks map[string]*widget.Check) fyne.CanvasObject {
	columns := make([]fyne.CanvasObject, 0, 2)
	for _, codes := range queryModalityColumns() {
		columnChecks := make([]fyne.CanvasObject, 0, len(codes))
		for _, code := range codes {
			if check := checks[code]; check != nil {
				columnChecks = append(columnChecks, container.NewGridWrap(
					fyne.NewSize(queryModalityCheckSlotWidth, check.MinSize().Height),
					check,
				))
			}
		}
		columns = append(columns, container.NewVBox(columnChecks...))
	}
	return container.NewGridWithColumns(2, columns...)
}

type queryDatePresetRadioGrid struct {
	groups    []*widget.RadioGroup
	selected  string
	updating  bool
	onChanged func(string)
}

func newQueryDatePresetRadioGrid(onChanged func(string)) *queryDatePresetRadioGrid {
	grid := &queryDatePresetRadioGrid{onChanged: onChanged}
	for groupIndex, options := range queryDatePresetColumns() {
		index := groupIndex
		group := widget.NewRadioGroup(options, func(preset string) {
			grid.selectFromGroup(index, preset)
		})
		group.Required = true
		grid.groups = append(grid.groups, group)
	}
	return grid
}

func (grid *queryDatePresetRadioGrid) CanvasObject() fyne.CanvasObject {
	if grid == nil {
		return container.NewGridWithColumns(3)
	}
	objects := make([]fyne.CanvasObject, 0, len(grid.groups))
	for _, group := range grid.groups {
		objects = append(objects, group)
	}
	return container.NewGridWithColumns(3, objects...)
}

func (grid *queryDatePresetRadioGrid) Selected() string {
	if grid == nil {
		return ""
	}
	return grid.selected
}

func (grid *queryDatePresetRadioGrid) SetSelected(preset string) {
	if grid == nil {
		return
	}
	grid.setSelected(preset, true)
}

func (grid *queryDatePresetRadioGrid) SetDisabled(disabled bool) {
	if grid == nil {
		return
	}
	for _, group := range grid.groups {
		setDisableableControl(group, disabled)
	}
}

func (grid *queryDatePresetRadioGrid) selectFromGroup(_ int, preset string) {
	if grid == nil || grid.updating || strings.TrimSpace(preset) == "" {
		return
	}
	grid.setSelected(preset, true)
}

func (grid *queryDatePresetRadioGrid) setSelected(preset string, notify bool) {
	if grid == nil || strings.TrimSpace(preset) == "" {
		return
	}
	grid.selected = preset
	grid.updating = true
	for _, group := range grid.groups {
		if stringSliceContains(group.Options, preset) {
			group.SetSelected(preset)
		} else {
			group.SetSelected("")
		}
	}
	grid.updating = false
	if notify && grid.onChanged != nil {
		grid.onChanged(preset)
	}
}

func stringSliceContains(values []string, value string) bool {
	for _, candidate := range values {
		if candidate == value {
			return true
		}
	}
	return false
}

func splitModalities(value string) []string {
	var modalities []string
	for _, part := range strings.Split(value, ",") {
		part = strings.TrimSpace(part)
		if part != "" {
			modalities = append(modalities, part)
		}
	}
	return modalities
}

func exportStudiesCSV(w fyne.Window, status *widget.Label, state *uiState) {
	if len(state.studies) == 0 {
		status.SetText("No studies to export")
		return
	}
	picker := dialog.NewFileSave(func(writer fyne.URIWriteCloser, err error) {
		if err != nil {
			status.SetText("Export failed")
			dialog.ShowError(err, w)
			return
		}
		if writer == nil {
			return
		}
		defer writer.Close()
		if err := studyexport.WriteStudiesCSV(writer, state.studies); err != nil {
			status.SetText("Export failed")
			dialog.ShowError(err, w)
			return
		}
		status.SetText(fmt.Sprintf("Exported %d studies to %s", len(state.studies), writer.URI().Name()))
	}, w)
	picker.SetFileName("go-pacs-studies.csv")
	picker.Show()
}

func exportStudiesJSON(w fyne.Window, status *widget.Label, state *uiState) {
	if len(state.studies) == 0 {
		status.SetText("No studies to export")
		return
	}
	picker := dialog.NewFileSave(func(writer fyne.URIWriteCloser, err error) {
		if err != nil {
			status.SetText("Export failed")
			dialog.ShowError(err, w)
			return
		}
		if writer == nil {
			return
		}
		defer writer.Close()
		if err := studyexport.WriteStudiesJSON(writer, state.studies); err != nil {
			status.SetText("Export failed")
			dialog.ShowError(err, w)
			return
		}
		status.SetText(fmt.Sprintf("Exported %d studies to %s", len(state.studies), writer.URI().Name()))
	}, w)
	picker.SetFileName("go-pacs-studies.json")
	picker.Show()
}

func exportSeriesCSV(w fyne.Window, status *widget.Label, state *uiState) {
	if len(state.series) == 0 {
		status.SetText("No series to export")
		return
	}
	picker := dialog.NewFileSave(func(writer fyne.URIWriteCloser, err error) {
		if err != nil {
			status.SetText("Export failed")
			dialog.ShowError(err, w)
			return
		}
		if writer == nil {
			return
		}
		defer writer.Close()
		if err := studyexport.WriteSeriesCSV(writer, state.series); err != nil {
			status.SetText("Export failed")
			dialog.ShowError(err, w)
			return
		}
		status.SetText(fmt.Sprintf("Exported %d series to %s", len(state.series), writer.URI().Name()))
	}, w)
	picker.SetFileName("go-pacs-series.csv")
	picker.Show()
}

func exportSeriesJSON(w fyne.Window, status *widget.Label, state *uiState) {
	if len(state.series) == 0 {
		status.SetText("No series to export")
		return
	}
	picker := dialog.NewFileSave(func(writer fyne.URIWriteCloser, err error) {
		if err != nil {
			status.SetText("Export failed")
			dialog.ShowError(err, w)
			return
		}
		if writer == nil {
			return
		}
		defer writer.Close()
		if err := studyexport.WriteSeriesJSON(writer, state.series); err != nil {
			status.SetText("Export failed")
			dialog.ShowError(err, w)
			return
		}
		status.SetText(fmt.Sprintf("Exported %d series to %s", len(state.series), writer.URI().Name()))
	}, w)
	picker.SetFileName("go-pacs-series.json")
	picker.Show()
}

func exportImagesCSV(w fyne.Window, status *widget.Label, state *uiState) {
	if len(state.instances) == 0 {
		status.SetText("No images to export")
		return
	}
	picker := dialog.NewFileSave(func(writer fyne.URIWriteCloser, err error) {
		if err != nil {
			status.SetText("Export failed")
			dialog.ShowError(err, w)
			return
		}
		if writer == nil {
			return
		}
		defer writer.Close()
		if err := studyexport.WriteInstancesCSV(writer, state.instances); err != nil {
			status.SetText("Export failed")
			dialog.ShowError(err, w)
			return
		}
		status.SetText(fmt.Sprintf("Exported %d images to %s", len(state.instances), writer.URI().Name()))
	}, w)
	picker.SetFileName("go-pacs-images.csv")
	picker.Show()
}

func exportImagesJSON(w fyne.Window, status *widget.Label, state *uiState) {
	if len(state.instances) == 0 {
		status.SetText("No images to export")
		return
	}
	picker := dialog.NewFileSave(func(writer fyne.URIWriteCloser, err error) {
		if err != nil {
			status.SetText("Export failed")
			dialog.ShowError(err, w)
			return
		}
		if writer == nil {
			return
		}
		defer writer.Close()
		if err := studyexport.WriteInstancesJSON(writer, state.instances); err != nil {
			status.SetText("Export failed")
			dialog.ShowError(err, w)
			return
		}
		status.SetText(fmt.Sprintf("Exported %d images to %s", len(state.instances), writer.URI().Name()))
	}, w)
	picker.SetFileName("go-pacs-images.json")
	picker.Show()
}

func refreshStudies(w fyne.Window, status *widget.Label, tables archiveTables, state *uiState) {
	studies, err := loadStudies(context.Background(), state)
	if err != nil {
		status.SetText("Refresh failed")
		dialog.ShowError(err, w)
		return
	}
	setStudies(state, tables, studies)
	status.SetText(fmt.Sprintf("%d studies in local archive", len(studies)))
}

func loadStudies(ctx context.Context, state *uiState) ([]archive.Study, error) {
	studies, err := state.catalog.StudiesWithFilters(ctx, state.studyFilters)
	if err != nil {
		return nil, err
	}
	if state != nil && state.selectedArchiveAlbum == archiveAlbumOpened {
		studies = filterStudiesByOpenedUIDs(studies, state.openedArchiveStudyUIDs)
	}
	return studies, nil
}

func setStudies(state *uiState, tables archiveTables, studies []archive.Study) {
	selectedStudyUID := selectedArchiveStudyUID(state)
	state.studies = studies
	retainedPatientGroups := retainCollapsedPatientGroups(state.collapsedPatientGroups, studies)
	state.collapsedPatientGroups = collapsePatientGroupsByDefault(retainedPatientGroups, studies, patientKeyForStudyUID(studies, selectedStudyUID))
	state.collapsedArchiveStudies = retainCollapsedArchiveStudies(state.collapsedArchiveStudies, studies)
	state.collapsedArchiveSeries = retainCollapsedArchiveSeries(state.collapsedArchiveSeries, state.archiveInstancesBySeries)
	applySavedArchiveSortPreferenceForSelectedUID(state, selectedStudyUID)
	if state.archiveRows == nil {
		state.archiveRows = archiveBrowserRowsForState(state)
	}
	state.selectedStudyRow = archiveStudyIndexByUID(studies, selectedStudyUID)
	clearArchiveDetails(state, tables)
	if tables.studies != nil {
		tables.studies.Refresh()
	}
	refreshArchiveChrome(state)
}

func retainCollapsedPatientGroups(collapsed map[string]bool, studies []archive.Study) map[string]bool {
	if len(collapsed) == 0 || len(studies) == 0 {
		return nil
	}
	valid := map[string]bool{}
	for _, study := range studies {
		valid[archivePatientKey(study)] = true
	}
	retained := map[string]bool{}
	for key, isCollapsed := range collapsed {
		// Keep both collapsed (true) and explicitly-expanded (false) entries for
		// patients still present, so a patient the user opened stays open across
		// refreshes while untracked patients fall back to the collapsed default.
		if valid[key] {
			retained[key] = isCollapsed
		}
	}
	if len(retained) == 0 {
		return nil
	}
	return retained
}

// collapsePatientGroupsByDefault marks every patient group collapsed unless it
// was explicitly tracked already, so the archive list opens showing one row per
// patient/exam with their studies hidden until the disclosure arrow is used.
// The patient that owns the selected study is kept expanded so the current
// selection stays visible.
func collapsePatientGroupsByDefault(existing map[string]bool, studies []archive.Study, expandedPatientKey string) map[string]bool {
	result := map[string]bool{}
	for key, collapsed := range existing {
		result[key] = collapsed
	}
	for _, study := range studies {
		key := archivePatientKey(study)
		if _, tracked := result[key]; !tracked {
			result[key] = true
		}
	}
	if expandedPatientKey != "" {
		result[expandedPatientKey] = false
	}
	if len(result) == 0 {
		return nil
	}
	return result
}

func patientKeyForStudyUID(studies []archive.Study, studyUID string) string {
	studyUID = strings.TrimSpace(studyUID)
	if studyUID == "" {
		return ""
	}
	for _, study := range studies {
		if strings.TrimSpace(study.StudyInstanceUID) == studyUID {
			return archivePatientKey(study)
		}
	}
	return ""
}

func retainCollapsedArchiveStudies(collapsed map[string]bool, studies []archive.Study) map[string]bool {
	if len(collapsed) == 0 || len(studies) == 0 {
		return nil
	}
	valid := map[string]bool{}
	for _, study := range studies {
		if studyUID := strings.TrimSpace(study.StudyInstanceUID); studyUID != "" {
			valid[studyUID] = true
		}
	}
	retained := map[string]bool{}
	for studyUID, isCollapsed := range collapsed {
		if isCollapsed && valid[strings.TrimSpace(studyUID)] {
			retained[studyUID] = true
		}
	}
	if len(retained) == 0 {
		return nil
	}
	return retained
}

func retainCollapsedArchiveSeries(collapsed map[string]bool, instancesBySeries map[string][]archive.Instance) map[string]bool {
	if len(collapsed) == 0 || len(instancesBySeries) == 0 {
		return nil
	}
	retained := map[string]bool{}
	for seriesUID, isCollapsed := range collapsed {
		key := strings.TrimSpace(seriesUID)
		if !isCollapsed || key == "" || len(instancesBySeries[key]) == 0 {
			continue
		}
		retained[key] = true
	}
	if len(retained) == 0 {
		return nil
	}
	return retained
}

func clearArchiveDetails(state *uiState, tables archiveTables) {
	state.series = nil
	state.instances = nil
	state.selectedSeriesRow = -1
	state.selectedInstanceRow = -1
	if tables.series != nil {
		tables.series.Refresh()
	}
	if tables.instances != nil {
		tables.instances.Refresh()
	}
	refreshArchiveChrome(state)
}

func setSeries(state *uiState, tables archiveTables, series []archive.Series) {
	state.series = series
	state.instances = nil
	state.selectedSeriesRow = -1
	state.selectedInstanceRow = -1
	applySavedSeriesSortPreference(state)
	if study, ok := selectedStudy(state); ok && strings.TrimSpace(study.StudyInstanceUID) != "" {
		if state.archiveSeriesByStudy == nil {
			state.archiveSeriesByStudy = map[string][]archive.Series{}
		}
		state.archiveSeriesByStudy[study.StudyInstanceUID] = series
		state.archiveRows = archiveBrowserRowsForState(state)
		tables.studies.Refresh()
	}
	tables.series.Refresh()
	tables.instances.Refresh()
	refreshArchiveChrome(state)
}

func setInstances(state *uiState, tables archiveTables, instances []archive.Instance) {
	state.instances = instances
	state.selectedInstanceRow = -1
	applySavedInstanceSortPreference(state)
	if series, ok := selectedSeries(state); ok {
		seriesUID := strings.TrimSpace(series.SeriesInstanceUID)
		if seriesUID != "" {
			if state.archiveInstancesBySeries == nil {
				state.archiveInstancesBySeries = map[string][]archive.Instance{}
			}
			if len(instances) == 0 {
				delete(state.archiveInstancesBySeries, seriesUID)
			} else {
				state.archiveInstancesBySeries[seriesUID] = instances
			}
			state.archiveRows = archiveBrowserRowsForState(state)
			if tables.studies != nil {
				tables.studies.Refresh()
			}
		}
	}
	if tables.instances != nil {
		tables.instances.Refresh()
	}
	refreshArchiveChrome(state)
}

func showStudyMetadataDialog(w fyne.Window, status *widget.Label, tables archiveTables, state *uiState) {
	study, ok := selectedStudy(state)
	if !ok {
		if status != nil {
			status.SetText("Select a study to edit")
		}
		return
	}
	statusEntry := widget.NewEntry()
	statusEntry.SetText(study.Status)
	statusPreset := widget.NewSelect(studyStatusPresetOptions(), func(label string) {
		if value := studyStatusPresetValue(label); value != "" {
			statusEntry.SetText(value)
		}
	})
	statusPreset.SetSelected(studyStatusPresetLabel(study.Status))
	commentsEntry := widget.NewMultiLineEntry()
	commentsEntry.SetText(study.Comments)
	form := dialog.NewForm(
		"Study Status/Comments",
		"Save",
		"Cancel",
		[]*widget.FormItem{
			widget.NewFormItem("Status Preset", statusPreset),
			widget.NewFormItem("Status", statusEntry),
			widget.NewFormItem("Comments", commentsEntry),
		},
		func(save bool) {
			if !save {
				return
			}
			metadata := archive.StudyMetadata{Status: statusEntry.Text, Comments: commentsEntry.Text}
			if err := saveSelectedStudyMetadata(context.Background(), status, tables, state, metadata); err != nil {
				if status != nil {
					status.SetText("Study metadata update failed")
				}
				dialog.ShowError(err, w)
			}
		},
		w,
	)
	form.Resize(fyne.NewSize(540, 360))
	form.Show()
}

func saveSelectedStudyMetadata(ctx context.Context, status *widget.Label, tables archiveTables, state *uiState, metadata archive.StudyMetadata) error {
	if state == nil || state.catalog == nil {
		return errors.New("archive catalog unavailable")
	}
	study, ok := selectedStudy(state)
	if !ok {
		if status != nil {
			status.SetText("Select a study to edit")
		}
		return errors.New("study selection required")
	}
	studyUID := strings.TrimSpace(study.StudyInstanceUID)
	if studyUID == "" {
		return errors.New("selected study has no Study Instance UID")
	}
	if err := state.catalog.SetStudyMetadata(ctx, studyUID, metadata); err != nil {
		return err
	}
	stored, err := state.catalog.StudyMetadata(ctx, studyUID)
	if err != nil {
		return err
	}
	state.studies[state.selectedStudyRow].Status = stored.Status
	state.studies[state.selectedStudyRow].Comments = stored.Comments
	if tables.studies != nil {
		tables.studies.Refresh()
	}
	refreshArchiveChrome(state)
	if status != nil {
		status.SetText("Updated study status/comments")
	}
	return nil
}

func showAnonymizeStudyDialog(w fyne.Window, status *widget.Label, tables archiveTables, state *uiState) {
	study, ok := selectedStudy(state)
	if !ok {
		if status != nil {
			status.SetText("Select a study to anonymize")
		}
		return
	}
	studyUID := strings.TrimSpace(study.StudyInstanceUID)
	if studyUID == "" {
		if status != nil {
			status.SetText("Selected study has no Study Instance UID")
		}
		return
	}
	message := fmt.Sprintf("Create an anonymized copy of study %s?", studyUID)
	if strings.TrimSpace(study.StudyDescription) != "" {
		message = fmt.Sprintf("%s\n\n%s", message, study.StudyDescription)
	}
	dialog.ShowConfirm("Anonymize Study", message, func(ok bool) {
		if ok {
			anonymizeSelectedStudy(w, status, tables, state)
		}
	}, w)
}

func anonymizeSelectedStudy(w fyne.Window, status *widget.Label, tables archiveTables, state *uiState) {
	if state == nil || state.session == nil {
		if status != nil {
			status.SetText("Archive session unavailable")
		}
		return
	}
	study, ok := selectedStudy(state)
	if !ok {
		if status != nil {
			status.SetText("Select a study to anonymize")
		}
		return
	}
	studyUID := strings.TrimSpace(study.StudyInstanceUID)
	if studyUID == "" {
		if status != nil {
			status.SetText("Selected study has no Study Instance UID")
		}
		return
	}
	if status != nil {
		status.SetText("Anonymizing study " + studyUID)
	}
	go func() {
		outcome, err := state.session.AnonymizeStudy(context.Background(), studyUID)
		studies, studyErr := loadStudies(context.Background(), state)
		fyne.Do(func() {
			if err != nil {
				if status != nil {
					status.SetText("Anonymize study failed")
				}
				dialog.ShowError(err, w)
				return
			}
			if studyErr != nil {
				if status != nil {
					status.SetText("Anonymize completed, refresh failed")
				}
				dialog.ShowError(studyErr, w)
				return
			}
			setStudies(state, tables, studies)
			if status != nil {
				status.SetText(anonymizedStudyStatusText(outcome))
			}
		})
	}()
}

func anonymizedStudyStatusText(outcome core.AnonymizeOutcome) string {
	return fmt.Sprintf("Anonymized study %s to %s (%d objects)", outcome.SourceStudyUID, outcome.NewStudyUID, outcome.StoredFiles)
}

func showDeleteStudyDialog(w fyne.Window, status *widget.Label, tables archiveTables, state *uiState) {
	study, ok := selectedStudy(state)
	if !ok {
		if status != nil {
			status.SetText("Select a study to delete")
		}
		return
	}
	studyUID := strings.TrimSpace(study.StudyInstanceUID)
	if studyUID == "" {
		if status != nil {
			status.SetText("Selected study has no Study Instance UID")
		}
		return
	}
	message := fmt.Sprintf("Delete study %s from the local archive?", studyUID)
	if strings.TrimSpace(study.StudyDescription) != "" {
		message = fmt.Sprintf("%s\n\n%s", message, study.StudyDescription)
	}
	dialog.ShowConfirm("Delete Study", message, func(ok bool) {
		if !ok {
			return
		}
		if _, err := deleteSelectedStudy(context.Background(), status, tables, state); err != nil {
			if status != nil {
				status.SetText("Delete study failed")
			}
			dialog.ShowError(err, w)
		}
	}, w)
}

func deleteSelectedStudy(ctx context.Context, status *widget.Label, tables archiveTables, state *uiState) (int, error) {
	if state == nil || state.catalog == nil {
		return 0, errors.New("archive catalog unavailable")
	}
	study, ok := selectedStudy(state)
	if !ok {
		if status != nil {
			status.SetText("Select a study to delete")
		}
		return 0, errors.New("study selection required")
	}
	studyUID := strings.TrimSpace(study.StudyInstanceUID)
	if studyUID == "" {
		return 0, errors.New("selected study has no Study Instance UID")
	}
	deleted, err := state.catalog.DeleteStudy(ctx, studyUID)
	if err != nil {
		return 0, err
	}
	if deleted == 0 {
		if status != nil {
			status.SetText("Study not found; nothing deleted")
		}
		return 0, fmt.Errorf("study %s was not found in the local archive", studyUID)
	}
	if state.archiveSeriesByStudy != nil {
		for _, series := range state.archiveSeriesByStudy[studyUID] {
			seriesUID := strings.TrimSpace(series.SeriesInstanceUID)
			if seriesUID == "" {
				continue
			}
			if state.archiveInstancesBySeries != nil {
				delete(state.archiveInstancesBySeries, seriesUID)
			}
			if state.collapsedArchiveSeries != nil {
				delete(state.collapsedArchiveSeries, seriesUID)
			}
		}
		delete(state.archiveSeriesByStudy, studyUID)
	}
	if state.collapsedArchiveStudies != nil {
		delete(state.collapsedArchiveStudies, studyUID)
	}
	studies, err := loadStudies(ctx, state)
	if err != nil {
		if status != nil {
			status.SetText("Study deleted; refresh failed")
		}
		return deleted, err
	}
	setStudies(state, tables, studies)
	if status != nil {
		status.SetText(deletedStudyStatusText(studyUID, deleted))
	}
	return deleted, nil
}

func deletedStudyStatusText(studyUID string, deleted int) string {
	noun := "objects"
	if deleted == 1 {
		noun = "object"
	}
	return fmt.Sprintf("Deleted study %s (%d %s)", studyUID, deleted, noun)
}

func localAETitle(state *uiState) string {
	if state == nil || strings.TrimSpace(state.appConfig.LocalAETitle) == "" {
		return netverify.DefaultCallingAETitle
	}
	return state.appConfig.LocalAETitle
}

func queryMoveDestination(state *uiState, node nodes.Node) string {
	if state != nil && strings.TrimSpace(state.queryMoveDestination) != "" {
		return strings.TrimSpace(state.queryMoveDestination)
	}
	if strings.TrimSpace(node.PreferredMoveDestination) != "" {
		return strings.TrimSpace(node.PreferredMoveDestination)
	}
	if state != nil && state.receiver != nil {
		if aeTitle := strings.TrimSpace(state.receiver.AETitle()); aeTitle != "" {
			return aeTitle
		}
	}
	return localAETitle(state)
}

func queryMoveDestinationOptions(state *uiState) []string {
	seen := map[string]bool{}
	var options []string
	appendOption := func(value string) {
		value = strings.TrimSpace(value)
		if value == "" || seen[value] {
			return
		}
		seen[value] = true
		options = append(options, value)
	}
	appendOption(localAETitle(state))
	if state != nil && state.receiver != nil {
		appendOption(state.receiver.AETitle())
	}
	for _, node := range querySourceNodes(state) {
		appendOption(node.PreferredMoveDestination)
	}
	if state != nil {
		appendOption(state.queryMoveDestination)
	}
	return options
}

func querySourcePriorityText(sources []nodes.Node) string {
	if len(sources) == 0 {
		return "no source selected"
	}
	if len(sources) == 1 {
		return "source " + sources[0].Name
	}
	names := make([]string, 0, len(sources))
	for _, node := range sources {
		name := strings.TrimSpace(node.Name)
		if name == "" {
			name = emptyDash(node.AETitle)
		}
		names = append(names, name)
	}
	return fmt.Sprintf("%d sources priority: %s", len(sources), strings.Join(names, " -> "))
}

func querySourceRetrieveMethodText(sources []nodes.Node) string {
	if len(sources) == 0 {
		return "Auto C-MOVE/C-GET"
	}
	method := sources[0].RetrieveMethodOrDefault()
	for _, source := range sources[1:] {
		if source.RetrieveMethodOrDefault() != method {
			return "mixed retrieve methods"
		}
	}
	return retrieveMethodSummary(sources[0])
}

func queryDestinationText(state *uiState) string {
	moveDestination := localAETitle(state)
	receiveAddress := ""
	source := "no source selected"
	method := "Auto C-MOVE/C-GET"
	if state != nil {
		receiveAddress = state.appConfig.ReceiverAddress
		if state.receiver != nil {
			snapshot := state.receiver.Snapshot()
			if strings.TrimSpace(snapshot.Address) != "" {
				receiveAddress = snapshot.Address
			}
			if strings.TrimSpace(snapshot.AETitle) != "" {
				moveDestination = snapshot.AETitle
			}
		}
		if sources := querySourceNodes(state); len(sources) > 1 {
			source = querySourcePriorityText(sources)
			method = querySourceRetrieveMethodText(sources)
			if override := strings.TrimSpace(state.queryMoveDestination); override != "" {
				moveDestination = override
			}
		} else if node, ok := selectedQueryNode(state); ok {
			source = "source " + node.Name
			moveDestination = queryMoveDestination(state, node)
			method = retrieveMethodSummary(node)
		} else if strings.TrimSpace(state.queryMoveDestination) != "" {
			moveDestination = strings.TrimSpace(state.queryMoveDestination)
		}
	}
	return fmt.Sprintf("Retrieve to: %s via %s (%s, %s)", emptyDash(moveDestination), emptyDash(receiveAddress), source, method)
}

func newQueryMoveDestinationEntry(state *uiState) *widget.SelectEntry {
	entry := widget.NewSelectEntry(queryMoveDestinationOptions(state))
	entry.SetPlaceHolder("Move destination AE")
	entry.OnChanged = func(value string) {
		if state != nil {
			state.queryMoveDestination = strings.TrimSpace(value)
		}
		refreshQueryDestination(state)
	}
	options := queryMoveDestinationOptions(state)
	if len(options) > 0 {
		entry.SetText(options[0])
	}
	return entry
}

func refreshQueryDestination(state *uiState) {
	if state == nil {
		return
	}
	if state.queryMoveDestinationSelect != nil {
		options := queryMoveDestinationOptions(state)
		state.queryMoveDestinationSelect.SetOptions(options)
		if strings.TrimSpace(state.queryMoveDestinationSelect.Text) == "" && len(options) > 0 {
			state.queryMoveDestinationSelect.SetText(options[0])
		}
		state.queryMoveDestinationSelect.Refresh()
	}
	if state.queryDestinationLabel != nil {
		state.queryDestinationLabel.SetText(queryDestinationText(state))
	}
}

func queryResultSummaryText(state *uiState) string {
	count := 0
	if state != nil {
		count = len(state.queries)
	}
	noun := queryResultSummaryNoun(state, count)
	source := queryResultSourceSummaryText(state)
	lines := []string{fmt.Sprintf("%d %s found", count, noun)}
	if source != "" {
		lines = append(lines, source)
	}
	if selected := querySelectedRetrieveSummaryText(state); selected != "" {
		lines = append(lines, selected)
	}
	return strings.Join(lines, "\n")
}

func queryResultSourceSummaryText(state *uiState) string {
	if sourceLabels := queryResultSourceLabels(state); len(sourceLabels) > 1 {
		return fmt.Sprintf("%d sources: %s", len(sourceLabels), strings.Join(sourceLabels, ", "))
	} else if len(sourceLabels) == 1 {
		return sourceLabels[0]
	}
	if node, ok := selectedQueryNode(state); ok {
		return queryNodeSourceSummaryLabel(node)
	}
	return ""
}

func queryResultSourceLabels(state *uiState) []string {
	if state == nil {
		return nil
	}
	seen := map[string]bool{}
	var labels []string
	for _, match := range state.queries {
		label := queryMatchSourceSummaryLabel(match)
		if label == "" || seen[label] {
			continue
		}
		seen[label] = true
		labels = append(labels, label)
	}
	return labels
}

func queryMatchSourceSummaryLabel(match query.Match) string {
	name := strings.TrimSpace(match.SourceNodeName)
	host := strings.TrimSpace(match.SourceHost)
	if name != "" {
		return name
	}
	if host != "" && match.SourcePort != 0 {
		return fmt.Sprintf("%s:%d", host, match.SourcePort)
	}
	return ""
}

func queryNodeSourceSummaryLabel(node nodes.Node) string {
	return fmt.Sprintf("%s / %s:%d", emptyDash(node.Name), emptyDash(node.Host), node.Port)
}

func queryResultSummaryNoun(state *uiState, count int) string {
	kind := queryRunStudy
	if state != nil {
		kind = state.lastQuery.kind
	}
	return queryRunKindNoun(kind, count)
}

func queryRunKindNoun(kind queryRunKind, count int) string {
	singular := "study"
	plural := "studies"
	switch kind {
	case queryRunPatient:
		singular, plural = "patient", "patients"
	case queryRunSeries:
		singular, plural = "series", "series"
	case queryRunImage:
		singular, plural = "image", "images"
	}
	if count == 1 {
		return singular
	}
	return plural
}

func querySelectedRetrieveSummaryText(state *uiState) string {
	match, ok := selectedQuery(state)
	if !ok {
		return ""
	}
	_, label, ok := queryRetrieveLevelAndLabel(match)
	if !ok {
		return ""
	}
	if status := queryRetrieveRowStatus(state, match); status != "" {
		return "Selected: " + status
	}
	source := ""
	if node, ok := nodeForQueryMatch(state, match); ok && strings.TrimSpace(node.Name) != "" {
		source = " from " + strings.TrimSpace(node.Name)
	}
	return "Selected: retrieve " + label + source
}

func queryRetrieveRowStatus(state *uiState, match query.Match) string {
	if state == nil || len(state.queryRetrieveRows) == 0 {
		return ""
	}
	return state.queryRetrieveRows[queryRetrieveStatusKey(match)]
}

func recordQueryRetrieveRowStatus(state *uiState, match query.Match, text string) {
	if state == nil {
		return
	}
	key := queryRetrieveStatusKey(match)
	if key == "" {
		return
	}
	if state.queryRetrieveRows == nil {
		state.queryRetrieveRows = map[string]string{}
	}
	state.queryRetrieveRows[key] = text
}

func queryRetrieveStatusKey(match query.Match) string {
	level, _, ok := queryRetrieveLevelAndLabel(match)
	if !ok {
		return ""
	}
	match.QueryRetrieveLevel = level
	return autoQueryRetrieveCandidateKey(match)
}

func refreshQueryResultSummary(state *uiState) {
	if state == nil || state.queryResultSummaryLabel == nil {
		return
	}
	state.queryResultSummaryLabel.SetText(queryResultSummaryText(state))
}

func querySelectedDetailsText(state *uiState) string {
	match, ok := selectedQuery(state)
	if !ok {
		return "No query result selected"
	}
	level := strings.ToUpper(strings.TrimSpace(match.QueryRetrieveLevel))
	if level == "" {
		level = "STUDY"
	}
	lines := []string{
		"Level: " + level,
		"Study UID: " + emptyDash(match.StudyInstanceUID),
		"Series UID: " + emptyDash(match.SeriesInstanceUID),
		"SOP Class UID: " + emptyDash(match.SOPClassUID),
		"SOP Instance UID: " + emptyDash(match.SOPInstanceUID),
		"Accession: " + emptyDash(match.AccessionNumber),
		"Referrer: " + emptyDash(match.ReferringPhysicianName),
		"Institution: " + emptyDash(match.InstitutionName),
		"Study Status: " + emptyDash(match.StudyStatusID),
		"Series #: " + emptyDash(match.SeriesNumber),
		"Instance #: " + emptyDash(match.InstanceNumber),
		"Source: " + emptyDash(queryCell(match, queryTableColumnSource)),
	}
	if localState := queryRowLocalStateText(match.LocalState); localState != "" {
		lines = append(lines, "Local State: "+localState)
	}
	if retrieveStatus := queryRetrieveRowStatus(state, match); retrieveStatus != "" {
		lines = append(lines, "Retrieve Status: "+retrieveStatus)
	}
	return strings.Join(lines, "\n")
}

func selectedQueryTechnicalValue(state *uiState, field string) string {
	match, ok := selectedQuery(state)
	if !ok {
		return ""
	}
	switch field {
	case "Study UID":
		return strings.TrimSpace(match.StudyInstanceUID)
	case "Series UID":
		return strings.TrimSpace(match.SeriesInstanceUID)
	case "SOP Class UID":
		return strings.TrimSpace(match.SOPClassUID)
	case "SOP Instance UID":
		return strings.TrimSpace(match.SOPInstanceUID)
	default:
		return ""
	}
}

func copySelectedQueryTechnicalValue(state *uiState, status *widget.Label, field string) {
	value := selectedQueryTechnicalValue(state, field)
	if value == "" {
		if status != nil {
			status.SetText(field + " is empty")
		}
		return
	}
	fyne.CurrentApp().Clipboard().SetContent(value)
	if status != nil {
		status.SetText("Copied " + field)
	}
}

func newQuerySelectedDetailsPanel(state *uiState, status *widget.Label) fyne.CanvasObject {
	state.querySelectedDetailsLabel = compactWorkbenchLabel()
	state.querySelectedDetailsLabel.SetText(querySelectedDetailsText(state))
	copyButton := func(field string) *widget.Button {
		return widget.NewButtonWithIcon("Copy "+field, theme.ContentCopyIcon(), func() {
			copySelectedQueryTechnicalValue(state, status, field)
		})
	}
	return container.NewBorder(
		nil,
		container.NewHBox(
			copyButton("Study UID"),
			copyButton("Series UID"),
			copyButton("SOP Class UID"),
			copyButton("SOP Instance UID"),
		),
		nil,
		nil,
		state.querySelectedDetailsLabel,
	)
}

func refreshQuerySelectedDetails(state *uiState) {
	if state == nil || state.querySelectedDetailsLabel == nil {
		return
	}
	state.querySelectedDetailsLabel.SetText(querySelectedDetailsText(state))
}

type querySourceListCell struct {
	*fyne.Container
	background   *canvas.Rectangle
	dragHandle   *widget.Icon
	check        *widget.Check
	nameLabel    *widget.Label
	addressLabel *widget.Label
	aeTitleLabel *widget.Label
	verifyDot    *canvas.Circle
	queryDot     *canvas.Circle
}

const querySourceEmptyLabel = "No remote sources configured"
const querySourcePriorityHandleSlotWidth float32 = 16
const sourceStatusDotSlotSize float32 = 14
const querySourceStatusDotsSlotWidth float32 = sourceStatusDotSlotSize*2 + 4

func newQuerySourceListCell() *querySourceListCell {
	background := canvas.NewRectangle(archiveOddRowColor)
	verifyDot := newSourceStatusDot()
	queryDot := newSourceStatusDot()
	dragHandle := widget.NewIcon(theme.MenuIcon())
	dragHandleSlot := container.NewGridWrap(fyne.NewSize(querySourcePriorityHandleSlotWidth, dragHandle.MinSize().Height), dragHandle)
	check := widget.NewCheck("", nil)
	nameLabel := widget.NewLabel("Source")
	nameLabel.Wrapping = fyne.TextTruncate
	addressLabel := widget.NewLabel("Address")
	addressLabel.Wrapping = fyne.TextTruncate
	aeTitleLabel := widget.NewLabel("AETitle")
	aeTitleLabel.Wrapping = fyne.TextTruncate
	columns := newSourceColumnGrid(nameLabel, addressLabel, aeTitleLabel)
	dots := container.NewHBox(sourceStatusDotBox(verifyDot), sourceStatusDotBox(queryDot))
	row := container.NewBorder(nil, nil, container.NewHBox(dragHandleSlot, check), dots, columns)
	return &querySourceListCell{
		Container:    container.NewStack(background, newCompactTableCellContent(row), newTableColumnDividerLayer(), newTableRowDividerLayer()),
		background:   background,
		dragHandle:   dragHandle,
		check:        check,
		nameLabel:    nameLabel,
		addressLabel: addressLabel,
		aeTitleLabel: aeTitleLabel,
		verifyDot:    verifyDot,
		queryDot:     queryDot,
	}
}

func newSourceStatusDot() *canvas.Circle {
	dot := canvas.NewCircle(sourceStatusIdleColor)
	dot.StrokeColor = color.NRGBA{R: 26, G: 26, B: 26, A: 255}
	dot.StrokeWidth = 1
	return dot
}

func sourceStatusDotBox(dot *canvas.Circle) fyne.CanvasObject {
	return container.NewGridWrap(fyne.NewSize(sourceStatusDotSlotSize, sourceStatusDotSlotSize), dot)
}

func newQuerySourceColumnHeader() fyne.CanvasObject {
	name := querySourceHeaderLabel("Name")
	address := querySourceHeaderLabel("Address")
	aeTitle := querySourceHeaderLabel("AETitle")
	leftSpacer := container.NewGridWrap(fyne.NewSize(36+querySourcePriorityHandleSlotWidth, 1), canvas.NewRectangle(color.Transparent))
	rightSpacer := container.NewGridWrap(fyne.NewSize(querySourceStatusDotsSlotWidth, 1), canvas.NewRectangle(color.Transparent))
	row := container.NewBorder(nil, nil, leftSpacer, rightSpacer, newSourceColumnGrid(name, address, aeTitle))
	background := canvas.NewRectangle(archiveHeaderRowColor)
	return container.NewStack(background, newCompactTableCellContent(row), newTableColumnDividerLayer(), newTableRowDividerLayer())
}

func newSourceColumnGrid(columns ...fyne.CanvasObject) *fyne.Container {
	cells := make([]fyne.CanvasObject, 0, len(columns))
	for index, column := range columns {
		cells = append(cells, newSourceColumnCell(column, index < len(columns)-1))
	}
	return container.NewGridWithColumns(len(cells), cells...)
}

func newSourceColumnCell(content fyne.CanvasObject, divider bool) fyne.CanvasObject {
	if !divider {
		return content
	}
	return container.NewStack(content, newTableColumnDividerLayer())
}

func querySourceHeaderLabel(text string) *widget.Label {
	label := widget.NewLabelWithStyle(text, fyne.TextAlignLeading, fyne.TextStyle{Bold: true})
	label.Wrapping = fyne.TextTruncate
	return label
}

const dicomNodesSourcePanelWidth float32 = 560
const compactSourceListRowHeight float32 = compactTableRowHeight + 8
const dicomNodesSourcePanelBodyHeight float32 = compactSourceListRowHeight * 7

func applyCompactSourceListRows(list *widget.List, count int) {
	if list == nil {
		return
	}
	for id := 0; id < count; id++ {
		list.SetItemHeight(widget.ListItemID(id), compactSourceListRowHeight)
	}
}

func newDicomNodesSourcePanel(header fyne.CanvasObject, body fyne.CanvasObject) *fyne.Container {
	bodySlot := container.NewGridWrap(fyne.NewSize(dicomNodesSourcePanelWidth, dicomNodesSourcePanelBodyHeight), body)
	panel := container.NewBorder(header, nil, nil, nil, bodySlot)
	panel.Resize(fyne.NewSize(dicomNodesSourcePanelWidth, 0))
	return panel
}

func configureQuerySourceCell(cell *querySourceListCell, state *uiState, id widget.ListItemID, onChanged func()) {
	if cell == nil {
		return
	}
	configureQuerySourceCheck(cell.check, state, id, onChanged)
	applyQuerySourceCellStyle(cell, id, state != nil && id == state.selectedNodeRow)
	configureQuerySourceLabels(cell, state, id)
	if state == nil || id < 0 || id >= len(state.nodes) {
		applySourceStatusDot(cell.verifyDot, sourceStatusIdleColor)
		applySourceStatusDot(cell.queryDot, sourceStatusIdleColor)
		return
	}
	node := state.nodes[id]
	applySourceStatusDot(cell.verifyDot, nodeVerifyStatusDotColor(state, node))
	applySourceStatusDot(cell.queryDot, querySourceStatusDotColor(state, node))
}

func applyQuerySourceCellStyle(cell *querySourceListCell, row int, selected bool) {
	if cell == nil || cell.background == nil {
		return
	}
	if selected {
		cell.background.FillColor = archiveSelectedRowColor
	} else if row%2 == 0 {
		cell.background.FillColor = archiveEvenRowColor
	} else {
		cell.background.FillColor = archiveOddRowColor
	}
	cell.background.Refresh()
}

func configureQuerySourceLabels(cell *querySourceListCell, state *uiState, id widget.ListItemID) {
	if cell == nil {
		return
	}
	if querySourceEmptyRow(state, id) {
		configureQuerySourcePlaceholderLabels(cell)
		return
	}
	if state == nil || id < 0 || id >= len(state.nodes) {
		clearQuerySourceLabels(cell)
		return
	}
	configureQuerySourceNodeLabels(cell, state.nodes[id], id == state.selectedNodeRow)
}

func querySourceEmptyRow(state *uiState, id widget.ListItemID) bool {
	return id == 0 && (state == nil || len(state.nodes) == 0)
}

func configureQuerySourcePlaceholderLabels(cell *querySourceListCell) {
	if cell == nil {
		return
	}
	cell.nameLabel.TextStyle = fyne.TextStyle{Italic: true}
	cell.addressLabel.TextStyle = fyne.TextStyle{}
	cell.aeTitleLabel.TextStyle = fyne.TextStyle{}
	cell.nameLabel.SetText(querySourceEmptyLabel)
	cell.addressLabel.SetText("")
	cell.aeTitleLabel.SetText("")
}

func clearQuerySourceLabels(cell *querySourceListCell) {
	if cell == nil {
		return
	}
	cell.nameLabel.TextStyle = fyne.TextStyle{}
	cell.addressLabel.TextStyle = fyne.TextStyle{}
	cell.aeTitleLabel.TextStyle = fyne.TextStyle{}
	cell.nameLabel.SetText("")
	cell.addressLabel.SetText("")
	cell.aeTitleLabel.SetText("")
}

func configureQuerySourceNodeLabels(cell *querySourceListCell, node nodes.Node, selected bool) {
	if cell == nil {
		return
	}
	name := node.Name
	if suffix := querySourceDisabledSuffix(node); suffix != "" {
		name += suffix
	}
	style := fyne.TextStyle{}
	if !node.Enabled() || !node.QueryEnabled() {
		style.Italic = true
	}
	cell.nameLabel.TextStyle = style
	cell.addressLabel.TextStyle = style
	cell.aeTitleLabel.TextStyle = style
	cell.nameLabel.SetText(name)
	cell.addressLabel.SetText(fmt.Sprintf("%s:%d", emptyDash(node.Host), node.Port))
	cell.aeTitleLabel.SetText(emptyDash(node.AETitle))
}

func applySourceStatusDot(dot *canvas.Circle, fill color.NRGBA) {
	if dot == nil {
		return
	}
	dot.FillColor = fill
	dot.Refresh()
}

func nodeVerifyStatusDotColor(state *uiState, node nodes.Node) color.NRGBA {
	if state == nil {
		return sourceStatusIdleColor
	}
	switch state.nodeVerifyStatuses[nodeVerifyKey(node)] {
	case nodeVerifyOK:
		return sourceStatusOKColor
	case nodeVerifyFail:
		return sourceStatusFailColor
	default:
		return sourceStatusIdleColor
	}
}

func querySourceStatusDotColor(state *uiState, node nodes.Node) color.NRGBA {
	if state == nil {
		return sourceStatusIdleColor
	}
	switch state.querySourceStatuses[nodeVerifyKey(node)] {
	case querySourceOK:
		return sourceStatusOKColor
	case querySourceFail:
		return sourceStatusFailColor
	default:
		return sourceStatusIdleColor
	}
}

func querySourceRows(state *uiState) []string {
	if state == nil || len(state.nodes) == 0 {
		return []string{querySourceEmptyLabel}
	}
	rows := make([]string, 0, len(state.nodes))
	for _, node := range state.nodes {
		prefix := "  "
		check := "[x]"
		if !node.Enabled() || !node.QueryEnabled() {
			check = "[ ]"
		}
		rows = append(rows, fmt.Sprintf("%s%s %s%s", prefix, check, querySourceMarkers(state, node), querySourceNodeLabel(node)))
	}
	return rows
}

func querySourceChecked(node nodes.Node) bool {
	return node.Enabled() && node.QueryEnabled()
}

func querySourceNodeLabel(node nodes.Node) string {
	label := fmt.Sprintf("%s %s:%d", node.Name, node.Host, node.Port)
	if aeTitle := strings.TrimSpace(node.AETitle); aeTitle != "" {
		label += " " + aeTitle
	}
	return label
}

func querySourceCheckLabel(state *uiState, index int) string {
	if state == nil || index < 0 || index >= len(state.nodes) {
		return ""
	}
	node := state.nodes[index]
	prefix := "  "
	return fmt.Sprintf("%s%s%s", prefix, querySourceNodeLabel(node), querySourceDisabledSuffix(node))
}

func querySourceDisabledSuffix(node nodes.Node) string {
	if !node.Enabled() {
		return " (disabled)"
	}
	if !node.QueryEnabled() {
		return " (query off)"
	}
	return ""
}

type autoQuerySourceEntry struct {
	node      nodes.Node
	nodeIndex int
	source    autoquery.Source
}

func autoQuerySourceEntries(state *uiState) []autoQuerySourceEntry {
	if state == nil || len(state.nodes) == 0 {
		return nil
	}
	sources := autoQuerySourcesForState(state)
	state.autoQuerySources = sources
	type nodeEntry struct {
		node  nodes.Node
		index int
	}
	nodeByKey := make(map[string]nodeEntry, len(state.nodes))
	for index, node := range state.nodes {
		nodeByKey[autoQueryNodeKey(node)] = nodeEntry{node: node, index: index}
	}
	var entries []autoQuerySourceEntry
	for _, source := range sources {
		node, ok := nodeByKey[autoQuerySourceKey(source)]
		if !ok {
			continue
		}
		entries = append(entries, autoQuerySourceEntry{node: node.node, nodeIndex: node.index, source: source})
	}
	return entries
}

func autoQuerySourceChecked(entry autoQuerySourceEntry) bool {
	return entry.source.Enabled && entry.node.Enabled() && entry.node.QueryEnabled()
}

func autoQuerySourceRows(state *uiState) []string {
	entries := autoQuerySourceEntries(state)
	if len(entries) == 0 {
		return []string{querySourceEmptyLabel}
	}
	rows := make([]string, 0, len(entries))
	for _, entry := range entries {
		prefix := "  "
		check := "[ ]"
		if autoQuerySourceChecked(entry) {
			check = "[x]"
		}
		rows = append(rows, fmt.Sprintf("%s%s %s%s", prefix, check, querySourceMarkers(state, entry.node), querySourceNodeLabel(entry.node)))
	}
	return rows
}

func autoQuerySourceCheckLabel(state *uiState, index int) string {
	entries := autoQuerySourceEntries(state)
	if state == nil || index < 0 || index >= len(entries) {
		return ""
	}
	node := entries[index].node
	prefix := "  "
	return fmt.Sprintf("%s%s%s", prefix, querySourceNodeLabel(node), querySourceDisabledSuffix(node))
}

func setAutoQuerySourceEnabled(state *uiState, row int, enabled bool) bool {
	entries := autoQuerySourceEntries(state)
	if state == nil || row < 0 || row >= len(entries) {
		return false
	}
	sources := autoQuerySourcesForState(state)
	if row >= len(sources) || sources[row].Enabled == enabled {
		return false
	}
	sources[row].Enabled = enabled
	state.autoQuerySources = sources
	return true
}

func moveAutoQuerySource(state *uiState, delta int) bool {
	sources := autoQuerySourcesForState(state)
	if state == nil || delta == 0 || len(sources) == 0 {
		return false
	}
	row := state.selectedAutoQuerySourceRow
	if row < 0 || row >= len(sources) {
		return false
	}
	nextRow := row + delta
	if nextRow < 0 || nextRow >= len(sources) {
		return false
	}
	sources[row], sources[nextRow] = sources[nextRow], sources[row]
	state.autoQuerySources = sources
	state.selectedAutoQuerySourceRow = nextRow
	return true
}

func autoQuerySourceNodes(state *uiState) []nodes.Node {
	entries := autoQuerySourceEntries(state)
	var out []nodes.Node
	for _, entry := range entries {
		if autoQuerySourceChecked(entry) {
			out = append(out, entry.node)
		}
	}
	return out
}

func configureAutoQuerySourceCheck(check *widget.Check, state *uiState, id widget.ListItemID, onChanged func()) {
	if check == nil {
		return
	}
	check.OnChanged = nil
	entries := autoQuerySourceEntries(state)
	if state == nil || id < 0 || id >= len(entries) {
		check.Text = ""
		check.SetChecked(false)
		check.Disable()
		check.Refresh()
		return
	}
	check.Enable()
	check.Text = ""
	check.SetChecked(autoQuerySourceChecked(entries[id]))
	if autoQueryProfileLocked(state) {
		check.Disable()
	}
	check.OnChanged = func(checked bool) {
		if autoQueryProfileLocked(state) {
			check.SetChecked(autoQuerySourceChecked(entries[id]))
			return
		}
		state.selectedAutoQuerySourceRow = id
		state.selectedNodeRow = entries[id].nodeIndex
		changed := setAutoQuerySourceEnabled(state, id, checked)
		if changed && onChanged != nil {
			onChanged()
		}
	}
	check.Refresh()
}

func configureAutoQuerySourceCell(cell *querySourceListCell, state *uiState, id widget.ListItemID, onChanged func()) {
	if cell == nil {
		return
	}
	configureAutoQuerySourceCheck(cell.check, state, id, onChanged)
	applyQuerySourceCellStyle(cell, id, state != nil && id == state.selectedAutoQuerySourceRow)
	entries := autoQuerySourceEntries(state)
	if autoQuerySourceEmptyRow(entries, id) {
		configureQuerySourcePlaceholderLabels(cell)
		applySourceStatusDot(cell.verifyDot, sourceStatusIdleColor)
		applySourceStatusDot(cell.queryDot, sourceStatusIdleColor)
		return
	}
	if state == nil || id < 0 || id >= len(entries) {
		clearQuerySourceLabels(cell)
		applySourceStatusDot(cell.verifyDot, sourceStatusIdleColor)
		applySourceStatusDot(cell.queryDot, sourceStatusIdleColor)
		return
	}
	node := entries[id].node
	configureQuerySourceNodeLabels(cell, node, id == state.selectedAutoQuerySourceRow)
	applySourceStatusDot(cell.verifyDot, nodeVerifyStatusDotColor(state, node))
	applySourceStatusDot(cell.queryDot, querySourceStatusDotColor(state, node))
}

func autoQuerySourceEmptyRow(entries []autoQuerySourceEntry, id widget.ListItemID) bool {
	return id == 0 && len(entries) == 0
}

func setQuerySourceEnabled(state *uiState, row int, enabled bool) (bool, error) {
	if state == nil || row < 0 || row >= len(state.nodes) {
		return false, nil
	}
	next := state.nodes[row]
	if enabled {
		next.Disabled = false
		next.QueryDisabled = false
	} else {
		next.QueryDisabled = true
	}
	if next == state.nodes[row] {
		return false, nil
	}
	original := state.nodes[row]
	state.nodes[row] = next
	if state.nodeStore != nil {
		if err := state.nodeStore.Save(state.nodes); err != nil {
			state.nodes[row] = original
			return true, err
		}
	}
	return true, nil
}

func setAllNodesEnabled(state *uiState, enabled bool) (bool, error) {
	if state == nil || len(state.nodes) == 0 {
		return false, nil
	}
	next := append([]nodes.Node(nil), state.nodes...)
	changed := false
	for index := range next {
		disabled := !enabled
		if next[index].Disabled != disabled {
			next[index].Disabled = disabled
			changed = true
		}
	}
	if !changed {
		return false, nil
	}
	original := append([]nodes.Node(nil), state.nodes...)
	state.nodes = next
	if state.nodeStore != nil {
		if err := state.nodeStore.Save(state.nodes); err != nil {
			state.nodes = original
			return true, err
		}
	}
	refreshNodeTableRows(state)
	return true, nil
}

func moveQuerySource(state *uiState, delta int) (bool, error) {
	if state == nil || delta == 0 || len(state.nodes) == 0 {
		return false, nil
	}
	row := state.selectedNodeRow
	if row < 0 || row >= len(state.nodes) {
		return false, nil
	}
	nextRow := row + delta
	if nextRow < 0 || nextRow >= len(state.nodes) {
		return false, nil
	}
	next := append([]nodes.Node(nil), state.nodes...)
	next[row], next[nextRow] = next[nextRow], next[row]
	original := state.nodes
	originalRow := state.selectedNodeRow
	state.nodes = next
	state.selectedNodeRow = nextRow
	if state.nodeStore != nil {
		if err := state.nodeStore.Save(state.nodes); err != nil {
			state.nodes = original
			state.selectedNodeRow = originalRow
			return true, err
		}
	}
	refreshNodeTableRows(state)
	return true, nil
}

func configureQuerySourceCheck(check *widget.Check, state *uiState, id widget.ListItemID, onChanged func()) {
	if check == nil {
		return
	}
	check.OnChanged = nil
	if state == nil || id < 0 || id >= len(state.nodes) {
		check.Text = ""
		check.SetChecked(false)
		check.Disable()
		check.Refresh()
		return
	}
	check.Enable()
	check.Text = ""
	check.SetChecked(querySourceChecked(state.nodes[id]))
	check.OnChanged = func(checked bool) {
		state.selectedNodeRow = id
		changed, err := setQuerySourceEnabled(state, id, checked)
		if err != nil {
			check.SetChecked(querySourceChecked(state.nodes[id]))
			return
		}
		if changed && onChanged != nil {
			onChanged()
		}
	}
	check.Refresh()
}

func nodeVerifyKey(node nodes.Node) string {
	return node.Key()
}

func querySourceNodeName(node nodes.Node) string {
	if name := strings.TrimSpace(node.Name); name != "" {
		return name
	}
	return fmt.Sprintf("%s:%d", emptyDash(node.Host), node.Port)
}

func recordNodeVerifyStatus(state *uiState, node nodes.Node, status nodeVerifyStatus) {
	if state == nil {
		return
	}
	if state.nodeVerifyStatuses == nil {
		state.nodeVerifyStatuses = map[string]nodeVerifyStatus{}
	}
	key := nodeVerifyKey(node)
	state.nodeVerifyStatuses[key] = status
	if state.nodeVerifyStatusTimes == nil {
		state.nodeVerifyStatusTimes = map[string]time.Time{}
	}
	state.nodeVerifyStatusTimes[key] = time.Now()
	historyStatus := sourceStatusHistoryOK
	if status == nodeVerifyFail {
		historyStatus = sourceStatusHistoryFail
	}
	appendSourceStatusHistoryAt(state, node, sourceStatusHistoryKindVerify, historyStatus, state.nodeVerifyStatusTimes[key])
}

func nodeVerifyStatusMarker(state *uiState, node nodes.Node) string {
	if state == nil {
		return ""
	}
	key := nodeVerifyKey(node)
	switch state.nodeVerifyStatuses[key] {
	case nodeVerifyOK:
		return sourceStatusMarker("✓", state.nodeVerifyStatusTimes[key])
	case nodeVerifyFail:
		return sourceStatusMarker("!", state.nodeVerifyStatusTimes[key])
	default:
		return ""
	}
}

func recordQuerySourceStatus(state *uiState, node nodes.Node, status querySourceStatus) {
	if state == nil {
		return
	}
	if state.querySourceStatuses == nil {
		state.querySourceStatuses = map[string]querySourceStatus{}
	}
	key := nodeVerifyKey(node)
	state.querySourceStatuses[key] = status
	if state.querySourceStatusTimes == nil {
		state.querySourceStatusTimes = map[string]time.Time{}
	}
	state.querySourceStatusTimes[key] = time.Now()
	historyStatus := sourceStatusHistoryOK
	if status == querySourceFail {
		historyStatus = sourceStatusHistoryFail
	}
	appendSourceStatusHistoryAt(state, node, sourceStatusHistoryKindQuery, historyStatus, state.querySourceStatusTimes[key])
}

func querySourceStatusMarker(state *uiState, node nodes.Node) string {
	if state == nil {
		return ""
	}
	key := nodeVerifyKey(node)
	switch state.querySourceStatuses[key] {
	case querySourceOK:
		return sourceStatusMarker("Q✓", state.querySourceStatusTimes[key])
	case querySourceFail:
		return sourceStatusMarker("Q!", state.querySourceStatusTimes[key])
	default:
		return ""
	}
}

func sourceStatusMarker(marker string, at time.Time) string {
	if at.IsZero() {
		return marker + " "
	}
	return marker + " " + at.Local().Format("15:04") + " "
}

func querySourceMarkers(state *uiState, node nodes.Node) string {
	return nodeVerifyStatusMarker(state, node) + querySourceStatusMarker(state, node)
}

func appendSourceStatusHistoryAt(state *uiState, node nodes.Node, kind sourceStatusHistoryKind, status string, at time.Time) {
	if state == nil {
		return
	}
	if at.IsZero() {
		at = time.Now()
	}
	entry := sourceStatusHistoryEntry{
		At:       at,
		NodeName: emptyDash(querySourceNodeName(node)),
		Kind:     kind,
		Status:   status,
	}
	state.querySourceHistory = append([]sourceStatusHistoryEntry{entry}, state.querySourceHistory...)
	if len(state.querySourceHistory) > sourceStatusHistoryLimit {
		state.querySourceHistory = state.querySourceHistory[:sourceStatusHistoryLimit]
	}
	refreshSourceStatusHistory(state)
}

func sourceStatusHistoryText(state *uiState) string {
	return strings.Join(sourceStatusHistoryLines(state), "\n")
}

func sourceStatusHistoryLines(state *uiState) []string {
	if state == nil || len(state.querySourceHistory) == 0 {
		return []string{"No source history"}
	}
	lines := make([]string, 0, len(state.querySourceHistory))
	for _, entry := range state.querySourceHistory {
		lines = append(lines, fmt.Sprintf("%s %s %s %s", entry.At.Local().Format("15:04"), emptyDash(entry.NodeName), entry.Kind, entry.Status))
	}
	return lines
}

func refreshSourceStatusHistory(state *uiState) {
	if state == nil || state.querySourceHistoryLabel == nil {
		return
	}
	state.querySourceHistoryLabel.SetText(sourceStatusHistoryText(state))
}

func refreshQuerySourceList(state *uiState) {
	if state == nil || state.querySourceList == nil {
		return
	}
	applyCompactSourceListRows(state.querySourceList, len(querySourceRows(state)))
	state.querySourceList.Refresh()
}

func beginRetrieve(state *uiState, nodeName string, activityLabel ...string) (context.Context, context.CancelFunc) {
	ctx, cancel := context.WithCancel(context.Background())
	state.activeRetrieveCancel = cancel
	state.retrieveActivityNode = nodeName
	state.retrieveActivityLabel = ""
	if len(activityLabel) > 0 {
		state.retrieveActivityLabel = strings.TrimSpace(activityLabel[0])
	}
	state.retrieveActivityProgress = retrieve.Progress{}
	refreshArchiveChrome(state)
	return ctx, cancel
}

func beginQueryActivity(state *uiState, label string) {
	if state == nil {
		return
	}
	state.activeQueryActivityLabel = strings.TrimSpace(label)
	state.activeQueryActivityProgress = queryActivityProgress{}
	state.activeQueryActivityHasProgress = false
	refreshArchiveChrome(state)
}

func recordQueryActivityProgress(state *uiState, progress queryActivityProgress) {
	if state == nil {
		return
	}
	state.activeQueryActivityProgress = progress
	state.activeQueryActivityHasProgress = true
	refreshArchiveChrome(state)
}

func clearActiveQueryActivity(state *uiState) {
	if state == nil {
		return
	}
	state.activeQueryActivityLabel = ""
	state.activeQueryActivityProgress = queryActivityProgress{}
	state.activeQueryActivityHasProgress = false
	refreshArchiveChrome(state)
}

func beginSendActivity(state *uiState, label string) {
	if state == nil {
		return
	}
	state.activeSendActivityLabel = strings.TrimSpace(label)
	state.activeSendActivityProgress = send.Progress{}
	state.activeSendActivityHasProgress = false
	refreshArchiveChrome(state)
}

func recordSendActivityProgress(state *uiState, progress send.Progress) {
	if state == nil {
		return
	}
	state.activeSendActivityProgress = progress
	state.activeSendActivityHasProgress = true
	refreshArchiveChrome(state)
}

func clearActiveSendActivity(state *uiState) {
	if state == nil {
		return
	}
	state.activeSendActivityLabel = ""
	state.activeSendActivityProgress = send.Progress{}
	state.activeSendActivityHasProgress = false
	refreshArchiveChrome(state)
}

func beginImportActivity(state *uiState, label string) {
	if state == nil {
		return
	}
	state.activeImportActivityLabel = strings.TrimSpace(label)
	state.activeImportActivityProgress = archive.ImportProgress{}
	state.activeImportActivityHasProgress = false
	refreshArchiveChrome(state)
}

func recordImportActivityProgress(state *uiState, progress archive.ImportProgress) {
	if state == nil {
		return
	}
	state.activeImportActivityProgress = progress
	state.activeImportActivityHasProgress = true
	refreshArchiveChrome(state)
}

func clearActiveImportActivity(state *uiState) {
	if state == nil {
		return
	}
	state.activeImportActivityLabel = ""
	state.activeImportActivityProgress = archive.ImportProgress{}
	state.activeImportActivityHasProgress = false
	refreshArchiveChrome(state)
}

func clearActiveRetrieve(state *uiState) {
	state.activeRetrieveCancel = nil
	state.retrieveActivityLabel = ""
	refreshArchiveChrome(state)
}

func cancelActiveRetrieve(status *widget.Label, state *uiState) {
	if state.activeRetrieveCancel == nil {
		status.SetText("No active retrieve")
		return
	}
	cancel := state.activeRetrieveCancel
	state.activeRetrieveCancel = nil
	cancel()
	status.SetText("Cancelling active retrieve")
	refreshArchiveChrome(state)
}

func retrieveProgressCallback(status *widget.Label, state *uiState, nodeName string) func(retrieve.Progress) {
	return func(update retrieve.Progress) {
		fyne.Do(func() {
			if state != nil {
				state.retrieveActivityNode = nodeName
				state.retrieveActivityProgress = update
				refreshArchiveChrome(state)
			}
			status.SetText(fmt.Sprintf(
				"Retrieve %s progress: status=0x%04X remaining %d completed %d failed %d warnings %d",
				nodeName,
				update.FinalStatus,
				update.Remaining,
				update.Completed,
				update.Failed,
				update.Warnings,
			))
		})
	}
}

func sendProgressCallback(state *uiState) func(send.Progress) {
	return func(update send.Progress) {
		fyne.Do(func() {
			recordSendActivityProgress(state, update)
		})
	}
}

func importProgressCallback(state *uiState) func(archive.ImportProgress) {
	return func(update archive.ImportProgress) {
		fyne.Do(func() {
			recordImportActivityProgress(state, update)
		})
	}
}

func queryProgressCallback(state *uiState) queryActivityProgressFunc {
	return func(update queryActivityProgress) {
		fyne.Do(func() {
			recordQueryActivityProgress(state, update)
		})
	}
}

func retrieveProgressFraction(progress retrieve.Progress) float64 {
	done := int(progress.Completed) + int(progress.Failed) + int(progress.Warnings)
	total := done + int(progress.Remaining)
	if total == 0 {
		return 0
	}
	return progressFraction(done, total)
}

func retrieveProgressKnown(progress retrieve.Progress) bool {
	done := int(progress.Completed) + int(progress.Failed) + int(progress.Warnings)
	return done+int(progress.Remaining) > 0
}

func queryProgressKnown(state *uiState) bool {
	if state == nil || !state.activeQueryActivityHasProgress {
		return false
	}
	return state.activeQueryActivityProgress.Total > 0
}

func queryProgressFraction(state *uiState) float64 {
	if !queryProgressKnown(state) {
		return 0
	}
	progress := state.activeQueryActivityProgress
	return progressFraction(progress.Attempted, progress.Total)
}

func sendProgressKnown(state *uiState) bool {
	if state == nil || !state.activeSendActivityHasProgress {
		return false
	}
	return state.activeSendActivityProgress.Total > 0
}

func sendProgressFraction(state *uiState) float64 {
	if !sendProgressKnown(state) {
		return 0
	}
	progress := state.activeSendActivityProgress
	return progressFraction(progress.Attempted, progress.Total)
}

func progressFraction(done int, total int) float64 {
	if total <= 0 || done <= 0 {
		return 0
	}
	if done >= total {
		return 1
	}
	return float64(done) / float64(total)
}

func retrieveProgressText(nodeName string, progress retrieve.Progress) string {
	nodeName = strings.TrimSpace(nodeName)
	if nodeName == "" {
		nodeName = "active"
	}
	done := int(progress.Completed) + int(progress.Failed) + int(progress.Warnings)
	total := done + int(progress.Remaining)
	if total == 0 {
		return fmt.Sprintf("Retrieving images... %s", nodeName)
	}
	return fmt.Sprintf("Retrieving images... %s %d/%d done, fail %d, warn %d", nodeName, done, total, progress.Failed, progress.Warnings)
}

func retrieveProgressDetail(activityLabel string, nodeName string, progress retrieve.Progress) string {
	activityLabel = strings.TrimSpace(activityLabel)
	nodeName = strings.TrimSpace(nodeName)
	if nodeName == "" {
		nodeName = "active"
	}
	detail := activityLabel
	if detail == "" {
		detail = nodeName
	}
	done := int(progress.Completed) + int(progress.Failed) + int(progress.Warnings)
	total := done + int(progress.Remaining)
	if total == 0 {
		return detail
	}
	detail = fmt.Sprintf("%s %d/%d img", detail, done, total)
	if progress.Failed > 0 {
		detail += fmt.Sprintf(", fail %d", progress.Failed)
	}
	if progress.Warnings > 0 {
		detail += fmt.Sprintf(", warn %d", progress.Warnings)
	}
	return detail
}

func retrieveMethodName(outcome retrieve.Outcome) string {
	if outcome.Method != "" {
		return outcome.Method
	}
	return retrieve.MethodMove
}

func retrieveOptionsForNode(status *widget.Label, state *uiState, node nodes.Node) retrieve.Options {
	moveDestination := queryMoveDestination(state, node)
	return retrieve.Options{
		CallingAETitle:      localAETitle(state),
		Method:              retrieveOptionMethod(node),
		MoveDestination:     moveDestination,
		ReceiveAddress:      state.appConfig.ReceiverAddress,
		Receiver:            state.receiver,
		MaxStoreObjectBytes: optionalInt64Value(state.appConfig.MaxStoreObjectBytes),
		OnProgress:          retrieveProgressCallback(status, state, node.Name),
	}
}

func retrieveOptionMethod(node nodes.Node) string {
	switch node.RetrieveMethodOrDefault() {
	case nodes.RetrieveMethodMove:
		return retrieve.MethodMove
	case nodes.RetrieveMethodGet:
		return retrieve.MethodGet
	default:
		return ""
	}
}

func retrieveMethodSummary(node nodes.Node) string {
	switch node.RetrieveMethodOrDefault() {
	case nodes.RetrieveMethodMove:
		return retrieve.MethodMove
	case nodes.RetrieveMethodGet:
		return retrieve.MethodGet
	default:
		return "Auto C-MOVE/C-GET"
	}
}

func retrieveReceiverAddressIssue(state *uiState, node nodes.Node) string {
	if isLoopbackHost(node.Host) {
		return ""
	}
	address := state.appConfig.ReceiverAddress
	if state.receiver != nil {
		address = state.receiver.Snapshot().Address
	}
	host, _, err := net.SplitHostPort(strings.TrimSpace(address))
	if err != nil || !isLoopbackHost(host) {
		return ""
	}
	return fmt.Sprintf(
		"Receiver address %s is loopback; remote node %s cannot C-STORE to it. Use 0.0.0.0:11113 and configure the remote Move Destination to this Mac's LAN IP.",
		address,
		node.Name,
	)
}

func receiverAddressParts(address string) (string, string) {
	address = strings.TrimSpace(address)
	defaultHost, defaultPort, _ := net.SplitHostPort(receive.DefaultAddress)
	if address == "" {
		return defaultHost, defaultPort
	}
	host, port, err := net.SplitHostPort(address)
	if err == nil {
		return host, port
	}
	return address, defaultPort
}

func receiverAddressFromParts(host string, port string) (string, error) {
	host = strings.TrimSpace(host)
	port = strings.TrimSpace(port)
	if host == "" {
		defaultHost, _, _ := net.SplitHostPort(receive.DefaultAddress)
		host = defaultHost
	}
	if port == "" {
		_, defaultPort, _ := net.SplitHostPort(receive.DefaultAddress)
		port = defaultPort
	}
	portValue, err := parsePort(port)
	if err != nil {
		return "", fmt.Errorf("receiver port: %w", err)
	}
	return net.JoinHostPort(host, fmt.Sprintf("%d", portValue)), nil
}

func listenerAddressSummaryText(addresses []string, port string) string {
	var visible []string
	for _, address := range addresses {
		address = strings.TrimSpace(address)
		if address == "" {
			continue
		}
		visible = append(visible, address)
	}
	if len(visible) == 0 {
		return "-"
	}
	return strings.Join(visible, ", ")
}

func listenerReachableEndpoints(addresses []string, port string) []string {
	port = strings.TrimSpace(port)
	if port == "" {
		_, port, _ = net.SplitHostPort(receive.DefaultAddress)
	}
	var endpoints []string
	for _, address := range addresses {
		address = strings.TrimSpace(address)
		if address == "" {
			continue
		}
		endpoints = append(endpoints, net.JoinHostPort(address, port))
	}
	return endpoints
}

func listenerHostSelectOptions(currentHost string, addresses []string) []string {
	seen := map[string]bool{}
	var options []string
	add := func(value string) {
		value = strings.TrimSpace(value)
		if value == "" || seen[value] {
			return
		}
		seen[value] = true
		options = append(options, value)
	}
	add(currentHost)
	for _, address := range addresses {
		add(address)
	}
	return options
}

func newListenerHostSelect(receiverHost *widget.Entry, addresses []string, refresh func()) *widget.Select {
	options := listenerHostSelectOptions(receiverHost.Text, addresses)
	selectWidget := widget.NewSelect(options, func(host string) {
		host = strings.TrimSpace(host)
		if receiverHost != nil && host != "" {
			receiverHost.SetText(host)
		}
		if refresh != nil {
			refresh()
		}
	})
	if len(options) > 0 {
		selectWidget.SetSelected(options[0])
	}
	return selectWidget
}

func newListenerAdvancedBindingControls(receiverHost *widget.Entry, hostSelect *widget.Select, aeAliases *widget.Entry, listenerStatus fyne.CanvasObject, copyAddressesButton *widget.Button) *widget.Accordion {
	detail := container.NewVBox(
		labeledControl("Listener Status", listenerStatus),
		labeledControl("Address Actions", listenerSettingsActionButtonSlot(copyAddressesButton)),
		labeledControl("Bind Host", receiverHost),
		labeledControl("Use Detected Host", hostSelect),
		labeledEntry("AE Aliases", aeAliases),
	)
	advanced := widget.NewAccordion(widget.NewAccordionItem(listenerAdvancedBindingTitle, detail))
	advanced.CloseAll()
	return advanced
}

func newListenerAddressHostBindingItems(addressActions fyne.CanvasObject, listenerAddressSummary *widget.Entry, hostNameEditButton *widget.Button, hostNameEntry *widget.Entry, advancedBinding fyne.CanvasObject) []*widget.FormItem {
	addressSlot := container.NewGridWrap(fyne.NewSize(listenerAddressEntrySlotWidth, listenerAddressSummary.MinSize().Height), listenerAddressSummary)
	hostNameSlot := container.NewGridWrap(fyne.NewSize(listenerAddressEntrySlotWidth, hostNameEntry.MinSize().Height), hostNameEntry)
	addressWidget := fyne.CanvasObject(addressSlot)
	if addressActions != nil {
		addressWidget = container.NewHBox(addressSlot, addressActions)
	}
	hostNameWidget := fyne.CanvasObject(hostNameSlot)
	if hostNameEditButton != nil {
		hostNameWidget = container.NewHBox(hostNameSlot, listenerSettingsActionButtonSlot(hostNameEditButton))
	}
	return []*widget.FormItem{
		widget.NewFormItem(settingsLabelAddressSummary, addressWidget),
		widget.NewFormItem(settingsLabelHostName, hostNameWidget),
		widget.NewFormItem("", advancedBinding),
	}
}

func listenerSettingsStatusText(aeTitle string, host string, port string, activate bool, running *receive.Snapshot) string {
	if running != nil {
		return fmt.Sprintf("Running: %s binding %s; stored %d objects", emptyDash(running.AETitle), emptyDash(running.Address), running.Stored)
	}
	address, err := receiverAddressFromParts(host, port)
	if err != nil {
		address = strings.TrimSpace(host)
		if strings.TrimSpace(port) != "" {
			address = net.JoinHostPort(address, strings.TrimSpace(port))
		}
	}
	stateText := "Stopped"
	if activate {
		stateText = "Will start on Save"
	}
	return fmt.Sprintf("%s: %s binding %s", stateText, emptyDash(aeTitle), emptyDash(address))
}

func newActivateListenerCheck(autoStart bool, running bool) *widget.Check {
	check := widget.NewCheck("Activate DICOM listener when Go PACS is running", nil)
	check.SetChecked(autoStart || running)
	return check
}

func newListenerActivationSection(activateListener fyne.CanvasObject) fyne.CanvasObject {
	return activateListener
}

func aeTitleFromHostName(hostname string) string {
	hostname = strings.TrimSpace(hostname)
	var builder strings.Builder
	lastSpace := false
	for _, r := range strings.ToUpper(hostname) {
		valid := (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9')
		if valid {
			builder.WriteRune(r)
			lastSpace = false
			continue
		}
		if builder.Len() == 0 || lastSpace {
			continue
		}
		builder.WriteRune(' ')
		lastSpace = true
	}
	aeTitle := strings.TrimSpace(builder.String())
	runes := []rune(aeTitle)
	if len(runes) > 16 {
		aeTitle = strings.TrimSpace(string(runes[:16]))
	}
	if aeTitle == "" {
		return netverify.DefaultCallingAETitle
	}
	if err := nodes.ValidateAETitle(aeTitle); err != nil {
		return netverify.DefaultCallingAETitle
	}
	return aeTitle
}

func newUseHostNameForAETitleCheck(localAE *widget.Entry, hostname string, refresh func()) *widget.Check {
	manualAETitle := ""
	if localAE != nil {
		manualAETitle = localAE.Text
	}
	check := widget.NewCheck("Use Host Name for AETitle", func(checked bool) {
		if localAE != nil {
			if checked {
				manualAETitle = localAE.Text
				localAE.SetText(aeTitleFromHostName(hostname))
				localAE.Disable()
			} else {
				localAE.Enable()
				localAE.SetText(manualAETitle)
			}
		}
		if refresh != nil {
			refresh()
		}
	})
	return check
}

func newListenerAETitleControls(localAE *widget.Entry, useHostNameAETitle *widget.Check) fyne.CanvasObject {
	localAESlot := container.NewGridWrap(fyne.NewSize(listenerPrimaryEntrySlotWidth, localAE.MinSize().Height), localAE)
	return container.NewHBox(localAESlot, useHostNameAETitle)
}

func newDisabledCheck(text string, checked bool) *widget.Check {
	check := widget.NewCheck(text, nil)
	check.SetChecked(checked)
	check.Disable()
	return check
}

func newDisabledRadioGroup(options []string, selected string) *widget.RadioGroup {
	radio := widget.NewRadioGroup(options, nil)
	radio.SetSelected(selected)
	radio.Disable()
	return radio
}

func newDisabledEntryText(text string) *widget.Entry {
	entry := widget.NewEntry()
	entry.SetText(text)
	entry.Disable()
	return entry
}

func newListenerAddressSummaryEntry(text string) *widget.Entry {
	return newDisabledEntryText(text)
}

func newReceiverPortControls(port string) (*widget.Entry, fyne.CanvasObject) {
	entry := widget.NewEntry()
	entry.SetText(port)
	entrySlot := container.NewGridWrap(fyne.NewSize(listenerPortEntrySlotWidth, entry.MinSize().Height), entry)
	hint := compactWorkbenchLabel()
	hint.SetText("(between 1 and 65535)")
	return entry, container.NewHBox(entrySlot, hint)
}

func newDICOMTimeoutControls(seconds string) (*widget.Entry, fyne.CanvasObject) {
	entry := widget.NewEntry()
	entry.SetText(seconds)
	entrySlot := container.NewGridWrap(fyne.NewSize(listenerTimeoutEntrySlotWidth, entry.MinSize().Height), entry)
	return entry, container.NewHBox(entrySlot, widget.NewLabel("seconds"))
}

func newListenerHostNameControls(hostname string) (*widget.Entry, *widget.Button) {
	hostname = strings.TrimSpace(hostname)
	if hostname == "" {
		hostname = "-"
	}
	hostNameEntry := newDisabledEntryText(hostname)
	return hostNameEntry, nil
}

func listenerSettingsActionButtonSlot(button *widget.Button) fyne.CanvasObject {
	if button == nil {
		return container.NewGridWrap(fyne.NewSize(listenerSettingsActionButtonSlotWidth, 1), canvas.NewRectangle(color.Transparent))
	}
	return container.NewGridWrap(fyne.NewSize(listenerSettingsActionButtonSlotWidth, button.MinSize().Height), button)
}

func newListenerAddressActionControls(copyButton *widget.Button) fyne.CanvasObject {
	return nil
}

func newListenerSettingsPanel(content fyne.CanvasObject) fyne.CanvasObject {
	return container.NewStack(
		canvas.NewRectangle(archiveOddRowColor),
		newCompactTableCellContent(content),
		newTableColumnDividerLayer(),
		newTableRowDividerLayer(),
	)
}

func newListenerCoreSettingsSection(items []*widget.FormItem) fyne.CanvasObject {
	rows := make([]fyne.CanvasObject, 0, len(items))
	for _, item := range items {
		if item == nil || item.Widget == nil {
			continue
		}
		if strings.TrimSpace(item.Text) == "" {
			rows = append(rows, item.Widget)
			continue
		}
		rows = append(rows, labeledControl(item.Text, item.Widget))
	}
	return newListenerSettingsPanel(container.NewVBox(rows...))
}

func newListenerSettingsSections(activateListener fyne.CanvasObject, coreSettingsItems []*widget.FormItem, tlsListenerSection fyne.CanvasObject, incomingFilesSection fyne.CanvasObject, safetyLimits fyne.CanvasObject) []*widget.FormItem {
	return []*widget.FormItem{
		widget.NewFormItem("", newListenerActivationSection(activateListener)),
		widget.NewFormItem("", newListenerCoreSettingsSection(coreSettingsItems)),
		widget.NewFormItem("", tlsListenerSection),
		widget.NewFormItem("", incomingFilesSection),
		widget.NewFormItem("", safetyLimits),
	}
}

func newListenerIncomingFilesRadio(decompressImages bool) *widget.RadioGroup {
	incomingFiles := widget.NewRadioGroup([]string{
		listenerIncomingDontModifyLabel,
		listenerIncomingDecompressPolicyLabel,
	}, nil)
	if decompressImages {
		incomingFiles.SetSelected(listenerIncomingDecompressPolicyLabel)
	} else {
		incomingFiles.SetSelected(listenerIncomingDontModifyLabel)
	}
	return incomingFiles
}

func newListenerIncomingPolicyControls() fyne.CanvasObject {
	return newListenerIncomingPolicyControlsWithRadio(newListenerIncomingFilesRadio(false))
}

func newListenerIncomingPolicyControlsWithRadio(incomingFiles *widget.RadioGroup) fyne.CanvasObject {
	scanSeconds := newDisabledEntryText("10")
	scanSecondsSlot := container.NewGridWrap(fyne.NewSize(listenerIncomingScanEntrySlotWidth, scanSeconds.MinSize().Height), scanSeconds)
	unreadableObjects := newDisabledRadioGroup([]string{
		"Delete it",
		"Move it to the NOT READABLE folder",
	}, "Move it to the NOT READABLE folder")
	unreadableIndent := container.NewGridWrap(fyne.NewSize(listenerIncomingUnreadableIndentWidth, 1), canvas.NewRectangle(color.Transparent))
	return container.NewVBox(
		container.NewHBox(widget.NewLabel("Check for new files every:"), scanSecondsSlot, widget.NewLabel("seconds")),
		container.NewHBox(widget.NewLabel("Incoming files:"), incomingFiles),
		widget.NewLabel("If Go PACS is not able to open a file received by the DICOM listener:"),
		container.NewHBox(unreadableIndent, unreadableObjects),
		newDisabledCheck("Replace existing files with the newly received files", false),
	)
}

func newListenerIncomingFilesSection(policy fyne.CanvasObject) fyne.CanvasObject {
	title := widget.NewLabelWithStyle("Incoming files", fyne.TextAlignLeading, fyne.TextStyle{Bold: true})
	return container.NewVBox(title, newListenerSettingsPanel(policy))
}

func newListenerTLSControls(tlsListener *widget.Check, settingsTapped func()) (*widget.Check, *widget.Button, fyne.CanvasObject) {
	if tlsListener == nil {
		tlsListener = widget.NewCheck("Activate DICOM TLS Listener", nil)
	}
	if settingsTapped == nil {
		settingsTapped = func() {}
	}
	tlsSettingsButton := widget.NewButton("TLS Settings", settingsTapped)
	settingsSlot := container.NewGridWrap(fyne.NewSize(listenerTLSSettingsButtonSlotWidth, tlsSettingsButton.MinSize().Height), tlsSettingsButton)
	return tlsListener, tlsSettingsButton, container.NewHBox(tlsListener, settingsSlot)
}

func newListenerTLSSection(controls fyne.CanvasObject) fyne.CanvasObject {
	return controls
}

func newListenerMetadataPolicyControls() fyne.CanvasObject {
	return container.NewVBox(
		newDisabledCheck("Store Destination AETitle in PrivateInformationCreatorUID (0002,0100)", false),
		newDisabledCheck("Store Source AETitle in SourceApplicationEntityTitle (0002,0016)", true),
		newDisabledCheck("Activate the DICOM Listener only if this user session is active (user switching)", false),
	)
}

func newListenerMetadataPolicySection(policy fyne.CanvasObject) fyne.CanvasObject {
	return policy
}

func reachableInterfaceAddressTexts(ifaceAddresses []net.Addr) []string {
	addresses := make([]string, 0, len(ifaceAddresses))
	seen := map[string]bool{}
	for _, ifaceAddress := range ifaceAddresses {
		var ip net.IP
		switch value := ifaceAddress.(type) {
		case *net.IPNet:
			ip = value.IP
		case *net.IPAddr:
			ip = value.IP
		}
		if ip == nil || ip.IsLoopback() || ip.IsUnspecified() || ip.IsMulticast() || ip.IsLinkLocalUnicast() {
			continue
		}
		if ipv4 := ip.To4(); ipv4 != nil {
			ip = ipv4
		} else {
			ip = ip.To16()
		}
		if ip == nil {
			continue
		}
		text := ip.String()
		if !seen[text] {
			seen[text] = true
			addresses = append(addresses, text)
		}
	}
	return addresses
}

func localReachableAddresses() []string {
	interfaces, err := net.Interfaces()
	if err != nil {
		return nil
	}
	var addresses []string
	seen := map[string]bool{}
	for _, iface := range interfaces {
		if iface.Flags&net.FlagUp == 0 || iface.Flags&net.FlagLoopback != 0 {
			continue
		}
		ifaceAddresses, err := iface.Addrs()
		if err != nil {
			continue
		}
		for _, text := range reachableInterfaceAddressTexts(ifaceAddresses) {
			if seen[text] {
				continue
			}
			seen[text] = true
			addresses = append(addresses, text)
		}
	}
	return addresses
}

func isLoopbackHost(host string) bool {
	host = strings.Trim(strings.ToLower(strings.TrimSpace(host)), "[]")
	if host == "localhost" {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

type listenerSafetyLimitEntries struct {
	maxFileImportBytes      *widget.Entry
	maxZipEntryBytes        *widget.Entry
	maxZipTotalBytes        *widget.Entry
	maxZipEntryCount        *widget.Entry
	maxStoreObjectBytes     *widget.Entry
	maxImportTotalFiles     *widget.Entry
	maxImportPathLength     *widget.Entry
	maxImportDirectoryDepth *widget.Entry
}

func newListenerAdvancedSafetyLimitsControls(entries listenerSafetyLimitEntries) *widget.Accordion {
	detail := container.NewVBox(
		container.NewGridWithColumns(2,
			labeledEntry("Max File Import Bytes", entries.maxFileImportBytes),
			labeledEntry("Max ZIP Entry Bytes", entries.maxZipEntryBytes),
		),
		container.NewGridWithColumns(2,
			labeledEntry("Max ZIP Total Bytes", entries.maxZipTotalBytes),
			labeledEntry("Max ZIP Entry Count", entries.maxZipEntryCount),
		),
		container.NewGridWithColumns(2,
			labeledEntry("Max Store Object Bytes", entries.maxStoreObjectBytes),
			labeledEntry("Max Import Total Files", entries.maxImportTotalFiles),
		),
		container.NewGridWithColumns(2,
			labeledEntry("Max Import Path Length", entries.maxImportPathLength),
			labeledEntry("Max Import Directory Depth", entries.maxImportDirectoryDepth),
		),
	)
	advanced := widget.NewAccordion(widget.NewAccordionItem(listenerAdvancedSafetyLimitsTitle, detail))
	advanced.CloseAll()
	return advanced
}

func showSettingsDialog(w fyne.Window, status *widget.Label, tables archiveTables, state *uiState) {
	localAE := widget.NewEntry()
	localAE.SetText(localAETitle(state))
	receiverHostValue, receiverPortValue := receiverAddressParts(state.appConfig.ReceiverAddress)
	receiverHost := widget.NewEntry()
	receiverHost.SetText(receiverHostValue)
	receiverPort, receiverPortControls := newReceiverPortControls(receiverPortValue)
	activateListener := newActivateListenerCheck(state.appConfig.ReceiverAutoStart, state.receiver != nil)
	hostName, _ := os.Hostname()
	listenerAddresses := localReachableAddresses()
	hostNameEntry, hostNameEditButton := newListenerHostNameControls(hostName)
	listenerAddressSummary := newListenerAddressSummaryEntry("")
	listenerStatus := compactWorkbenchLabel()
	runningSnapshot := func() *receive.Snapshot {
		if state.receiver == nil {
			return nil
		}
		snapshot := state.receiver.Snapshot()
		return &snapshot
	}
	updateListenerStatus := func() {
		listenerStatus.SetText(listenerSettingsStatusText(localAE.Text, receiverHost.Text, receiverPort.Text, activateListener.Checked, runningSnapshot()))
	}
	updateListenerAddressSummary := func() {
		listenerAddressSummary.SetText(listenerAddressSummaryText(listenerAddresses, receiverPort.Text))
	}
	refreshListenerAddressControls := func() {
		updateListenerAddressSummary()
		updateListenerStatus()
	}
	hostSelect := newListenerHostSelect(receiverHost, listenerAddresses, refreshListenerAddressControls)
	additionalAEs := widget.NewEntry()
	additionalAEs.SetText(strings.Join(state.appConfig.AdditionalAETitles, ", "))
	copyAddressesButton := compactToolbarButton("Copy", theme.ContentCopyIcon(), func() {
		endpoints := listenerReachableEndpoints(listenerAddresses, receiverPort.Text)
		if len(endpoints) == 0 {
			status.SetText("No reachable listener addresses to copy")
			return
		}
		fyne.CurrentApp().Clipboard().SetContent(strings.Join(endpoints, "\n"))
		status.SetText("Copied listener addresses")
	})
	advancedBinding := newListenerAdvancedBindingControls(receiverHost, hostSelect, additionalAEs, listenerStatus, copyAddressesButton)
	addressActions := newListenerAddressActionControls(copyAddressesButton)
	receiverPort.OnChanged = func(_ string) {
		refreshListenerAddressControls()
	}
	receiverHost.OnChanged = func(_ string) {
		updateListenerStatus()
	}
	localAE.OnChanged = func(_ string) {
		updateListenerStatus()
	}
	activateListener.OnChanged = func(_ bool) {
		updateListenerStatus()
	}
	updateListenerAddressSummary()
	updateListenerStatus()
	useHostNameAETitle := newUseHostNameForAETitleCheck(localAE, hostName, updateListenerStatus)
	preferredReceiveSyntax, _, preferredReceiveSyntaxControls := newPreferredReceiveSyntaxControls(state.appConfig.ReceivePreferredTransferSyntax)
	dicomCommunicationTimeout, dicomCommunicationTimeoutControls := newDICOMTimeoutControls(strconv.Itoa(timeoutSecondsOrDefault(state.appConfig.DICOMCommunicationTimeoutSeconds, appconfig.DefaultDICOMCommunicationTimeoutSeconds)))
	dicomConnectionTimeout, dicomConnectionTimeoutControls := newDICOMTimeoutControls(strconv.Itoa(timeoutSecondsOrDefault(state.appConfig.DICOMConnectionTimeoutSeconds, appconfig.DefaultDICOMConnectionTimeoutSeconds)))
	receiverTLSCertFile := state.appConfig.ReceiverTLSCertFile
	receiverTLSKeyFile := state.appConfig.ReceiverTLSKeyFile
	tlsListener := widget.NewCheck("Activate DICOM TLS Listener", nil)
	tlsListener.SetChecked(state.appConfig.ReceiverUseTLS)
	showTLSSettings := func() {
		certFile := widget.NewEntry()
		certFile.SetText(receiverTLSCertFile)
		keyFile := widget.NewEntry()
		keyFile.SetText(receiverTLSKeyFile)
		tlsForm := dialog.NewForm("TLS Settings", "Save", "Cancel", []*widget.FormItem{
			widget.NewFormItem("Certificate File", certFile),
			widget.NewFormItem("Key File", keyFile),
		}, func(ok bool) {
			if !ok {
				return
			}
			receiverTLSCertFile = strings.TrimSpace(certFile.Text)
			receiverTLSKeyFile = strings.TrimSpace(keyFile.Text)
			status.SetText("TLS settings staged")
		}, w)
		tlsForm.Resize(fyne.NewSize(560, 180))
		tlsForm.Show()
	}
	_, _, tlsListenerControls := newListenerTLSControls(tlsListener, showTLSSettings)
	tlsListenerSection := newListenerTLSSection(tlsListenerControls)
	incomingFiles := newListenerIncomingFilesRadio(state.appConfig.ReceiveDecompressImages)
	incomingPolicy := newListenerIncomingPolicyControlsWithRadio(incomingFiles)
	incomingFilesSection := newListenerIncomingFilesSection(incomingPolicy)
	metadataPolicy := newListenerMetadataPolicyControls()
	metadataPolicySection := newListenerMetadataPolicySection(metadataPolicy)
	maxFileImportBytes := widget.NewEntry()
	maxFileImportBytes.SetText(formatOptionalInt64(state.appConfig.MaxFileImportBytes))
	maxZipEntryBytes := widget.NewEntry()
	maxZipEntryBytes.SetText(formatOptionalInt64(state.appConfig.MaxZipEntryBytes))
	maxZipTotalBytes := widget.NewEntry()
	maxZipTotalBytes.SetText(formatOptionalInt64(state.appConfig.MaxZipTotalBytes))
	maxZipEntryCount := widget.NewEntry()
	maxZipEntryCount.SetText(formatOptionalInt(state.appConfig.MaxZipEntryCount))
	maxStoreObjectBytes := widget.NewEntry()
	maxStoreObjectBytes.SetText(formatOptionalInt64(state.appConfig.MaxStoreObjectBytes))
	maxImportTotalFiles := widget.NewEntry()
	maxImportTotalFiles.SetText(formatOptionalInt(state.appConfig.MaxImportTotalFiles))
	maxImportPathLength := widget.NewEntry()
	maxImportPathLength.SetText(formatOptionalInt(state.appConfig.MaxImportPathLength))
	maxImportDirectoryDepth := widget.NewEntry()
	maxImportDirectoryDepth.SetText(formatOptionalInt(state.appConfig.MaxImportDirectoryDepth))
	safetyLimits := newListenerAdvancedSafetyLimitsControls(listenerSafetyLimitEntries{
		maxFileImportBytes:      maxFileImportBytes,
		maxZipEntryBytes:        maxZipEntryBytes,
		maxZipTotalBytes:        maxZipTotalBytes,
		maxZipEntryCount:        maxZipEntryCount,
		maxStoreObjectBytes:     maxStoreObjectBytes,
		maxImportTotalFiles:     maxImportTotalFiles,
		maxImportPathLength:     maxImportPathLength,
		maxImportDirectoryDepth: maxImportDirectoryDepth,
	})

	coreSettingsItems := []*widget.FormItem{
		widget.NewFormItem(settingsLabelAETitle, newListenerAETitleControls(localAE, useHostNameAETitle)),
		widget.NewFormItem(settingsLabelReceiverPort, receiverPortControls),
	}
	coreSettingsItems = append(coreSettingsItems, newListenerAddressHostBindingItems(addressActions, listenerAddressSummary, hostNameEditButton, hostNameEntry, advancedBinding)...)
	coreSettingsItems = append(coreSettingsItems,
		widget.NewFormItem(settingsLabelPreferredSyntax, preferredReceiveSyntaxControls),
		widget.NewFormItem("", metadataPolicySection),
		widget.NewFormItem(settingsLabelDICOMCommunicationTimeout, dicomCommunicationTimeoutControls),
		widget.NewFormItem(settingsLabelDICOMConnectionTimeout, dicomConnectionTimeoutControls),
	)
	formItems := newListenerSettingsSections(activateListener, coreSettingsItems, tlsListenerSection, incomingFilesSection, safetyLimits)

	form := dialog.NewForm("Settings", "Save", "Cancel", formItems, func(ok bool) {
		if !ok {
			return
		}
		communicationTimeout, err := parsePositiveSeconds(dicomCommunicationTimeout.Text, "DICOM communications timeout")
		if err != nil {
			status.SetText("Settings save failed")
			dialog.ShowError(err, w)
			return
		}
		connectionTimeout, err := parsePositiveSeconds(dicomConnectionTimeout.Text, "connection timeout")
		if err != nil {
			status.SetText("Settings save failed")
			dialog.ShowError(err, w)
			return
		}
		fileLimit, err := parseOptionalInt64Limit(maxFileImportBytes.Text, "max file import bytes")
		if err != nil {
			status.SetText("Settings save failed")
			dialog.ShowError(err, w)
			return
		}
		zipEntryLimit, err := parseOptionalInt64Limit(maxZipEntryBytes.Text, "max ZIP entry bytes")
		if err != nil {
			status.SetText("Settings save failed")
			dialog.ShowError(err, w)
			return
		}
		zipTotalLimit, err := parseOptionalInt64Limit(maxZipTotalBytes.Text, "max ZIP total bytes")
		if err != nil {
			status.SetText("Settings save failed")
			dialog.ShowError(err, w)
			return
		}
		zipEntryCount, err := parseOptionalIntLimit(maxZipEntryCount.Text, "max ZIP entry count")
		if err != nil {
			status.SetText("Settings save failed")
			dialog.ShowError(err, w)
			return
		}
		storeObjectLimit, err := parseOptionalInt64Limit(maxStoreObjectBytes.Text, "max store object bytes")
		if err != nil {
			status.SetText("Settings save failed")
			dialog.ShowError(err, w)
			return
		}
		importTotalFiles, err := parseOptionalIntLimit(maxImportTotalFiles.Text, "max import total files")
		if err != nil {
			status.SetText("Settings save failed")
			dialog.ShowError(err, w)
			return
		}
		importPathLength, err := parseOptionalIntLimit(maxImportPathLength.Text, "max import path length")
		if err != nil {
			status.SetText("Settings save failed")
			dialog.ShowError(err, w)
			return
		}
		importDirectoryDepth, err := parseOptionalIntLimit(maxImportDirectoryDepth.Text, "max import directory depth")
		if err != nil {
			status.SetText("Settings save failed")
			dialog.ShowError(err, w)
			return
		}
		receiverAddress, err := receiverAddressFromParts(receiverHost.Text, receiverPort.Text)
		if err != nil {
			status.SetText("Settings save failed")
			dialog.ShowError(err, w)
			return
		}
		cfg := appconfig.Config{
			LocalAETitle:                     localAE.Text,
			ReceiverAddress:                  receiverAddress,
			ReceiverAutoStart:                activateListener.Checked,
			ReceiverUseTLS:                   tlsListener.Checked,
			ReceiverTLSCertFile:              receiverTLSCertFile,
			ReceiverTLSKeyFile:               receiverTLSKeyFile,
			AdditionalAETitles:               parseAETitleList(additionalAEs.Text),
			ReceivePreferredTransferSyntax:   receivePreferredSyntaxValue(preferredReceiveSyntax.Selected),
			ReceiveDecompressImages:          incomingFiles.Selected == listenerIncomingDecompressPolicyLabel,
			DICOMCommunicationTimeoutSeconds: communicationTimeout,
			DICOMConnectionTimeoutSeconds:    connectionTimeout,
			MaxFileImportBytes:               fileLimit,
			MaxZipEntryBytes:                 zipEntryLimit,
			MaxZipTotalBytes:                 zipTotalLimit,
			MaxZipEntryCount:                 zipEntryCount,
			MaxStoreObjectBytes:              storeObjectLimit,
			MaxImportTotalFiles:              importTotalFiles,
			MaxImportPathLength:              importPathLength,
			MaxImportDirectoryDepth:          importDirectoryDepth,
		}
		if err := appconfig.Save(state.appConfigPath, cfg); err != nil {
			status.SetText("Settings save failed")
			dialog.ShowError(err, w)
			return
		}
		normalized, err := appconfig.Load(state.appConfigPath)
		if err != nil {
			status.SetText("Settings reload failed")
			dialog.ShowError(err, w)
			return
		}
		state.appConfig = normalized
		refreshArchiveChrome(state)
		refreshQueryDestination(state)
		refreshQueryResultSummary(state)
		refreshQuerySourceList(state)
		if state.appConfig.ReceiverAutoStart && state.receiver == nil {
			startReceiver(w, status, state)
			return
		}
		if !state.appConfig.ReceiverAutoStart && state.receiver != nil {
			stopReceiver(w, status, tables, state)
			return
		}
		if state.receiver != nil {
			status.SetText("Settings saved; restart receiver to apply listener changes")
			return
		}
		status.SetText("Settings saved")
	}, w)
	form.Resize(listenerSettingsDialogSize())
	form.Show()
}

func importOptionsFromConfig(cfg appconfig.Config) archive.ImportOptions {
	return archive.ImportOptions{
		Limits: archive.ImportLimits{
			MaxFileImportBytes:      optionalInt64Value(cfg.MaxFileImportBytes),
			MaxZipEntryBytes:        optionalInt64Value(cfg.MaxZipEntryBytes),
			MaxZipTotalBytes:        optionalInt64Value(cfg.MaxZipTotalBytes),
			MaxZipEntryCount:        optionalIntValue(cfg.MaxZipEntryCount),
			MaxImportTotalFiles:     optionalIntValue(cfg.MaxImportTotalFiles),
			MaxImportPathLength:     optionalIntValue(cfg.MaxImportPathLength),
			MaxImportDirectoryDepth: optionalIntValue(cfg.MaxImportDirectoryDepth),
		},
	}
}

func optionalInt64Value(value *int64) int64 {
	if value == nil {
		return 0
	}
	return *value
}

func optionalIntValue(value *int) int {
	if value == nil {
		return 0
	}
	return *value
}

func receivePreferredSyntaxLabels() []string {
	return []string{
		receivePreferredSyntaxAutoLabel,
		receivePreferredSyntaxExplicitLabel,
		receivePreferredSyntaxImplicitLabel,
	}
}

func receivePreferredSyntaxLabel(value string) string {
	switch strings.TrimSpace(value) {
	case receive.PreferredTransferSyntaxExplicitVRLittleEndian:
		return receivePreferredSyntaxExplicitLabel
	case receive.PreferredTransferSyntaxImplicitVRLittleEndian:
		return receivePreferredSyntaxImplicitLabel
	default:
		return receivePreferredSyntaxAutoLabel
	}
}

func receivePreferredSyntaxValue(label string) string {
	switch strings.TrimSpace(label) {
	case receivePreferredSyntaxExplicitLabel:
		return receive.PreferredTransferSyntaxExplicitVRLittleEndian
	case receivePreferredSyntaxImplicitLabel:
		return receive.PreferredTransferSyntaxImplicitVRLittleEndian
	default:
		return receive.PreferredTransferSyntaxAuto
	}
}

func newPreferredReceiveSyntaxControls(selected string) (*widget.Select, *widget.Label, fyne.CanvasObject) {
	selectWidget := widget.NewSelect(receivePreferredSyntaxLabels(), nil)
	selectWidget.SetSelected(receivePreferredSyntaxLabel(selected))
	hint := compactWorkbenchDetailLabel()
	hint.SetText("(used during Q/R Retrieve)")
	selectSlot := container.NewGridWrap(fyne.NewSize(listenerPreferredSyntaxSlotWidth, selectWidget.MinSize().Height), selectWidget)
	return selectWidget, hint, container.NewHBox(selectSlot, hint)
}

func sendSyntaxOptions() []string {
	return []string{
		sendSyntaxAutoLabel,
		sendSyntaxExplicitLabel,
		sendSyntaxImplicitLabel,
	}
}

func sendSyntaxLabel(value string) string {
	switch strings.TrimSpace(value) {
	case nodes.SendTransferSyntaxExplicitVRLittleEndian:
		return sendSyntaxExplicitLabel
	case nodes.SendTransferSyntaxImplicitVRLittleEndian:
		return sendSyntaxImplicitLabel
	default:
		return sendSyntaxAutoLabel
	}
}

func sendSyntaxTableLabel(value string) string {
	switch strings.TrimSpace(value) {
	case nodes.SendTransferSyntaxExplicitVRLittleEndian, nodes.SendTransferSyntaxImplicitVRLittleEndian:
		return sendSyntaxLabel(value)
	default:
		return ""
	}
}

func sendSyntaxValue(label string) string {
	switch strings.TrimSpace(label) {
	case sendSyntaxExplicitLabel:
		return nodes.SendTransferSyntaxExplicitVRLittleEndian
	case sendSyntaxImplicitLabel:
		return nodes.SendTransferSyntaxImplicitVRLittleEndian
	default:
		return nodes.SendTransferSyntaxAuto
	}
}

func studyStatusPresetOptions() []string {
	return []string{
		studyStatusPresetCustomLabel,
		studyStatusPresetReviewedLabel,
		studyStatusPresetInterestingLabel,
		studyStatusPresetFollowUpLabel,
		studyStatusPresetTeachingLabel,
		studyStatusPresetProblemLabel,
	}
}

func studyStatusPresetValue(label string) string {
	switch strings.TrimSpace(label) {
	case studyStatusPresetReviewedLabel:
		return studyStatusPresetReviewedLabel
	case studyStatusPresetInterestingLabel:
		return studyStatusPresetInterestingLabel
	case studyStatusPresetFollowUpLabel:
		return studyStatusPresetFollowUpLabel
	case studyStatusPresetTeachingLabel:
		return studyStatusPresetTeachingLabel
	case studyStatusPresetProblemLabel:
		return studyStatusPresetProblemLabel
	default:
		return ""
	}
}

func studyStatusPresetLabel(status string) string {
	status = strings.TrimSpace(status)
	for _, option := range studyStatusPresetOptions() {
		if option != studyStatusPresetCustomLabel && strings.EqualFold(status, option) {
			return option
		}
	}
	return studyStatusPresetCustomLabel
}

func timeoutSecondsOrDefault(value int, defaultValue int) int {
	if value > 0 {
		return value
	}
	return defaultValue
}

func dicomCommunicationTimeoutDuration(cfg appconfig.Config) time.Duration {
	seconds := timeoutSecondsOrDefault(cfg.DICOMCommunicationTimeoutSeconds, appconfig.DefaultDICOMCommunicationTimeoutSeconds)
	return time.Duration(seconds) * time.Second
}

func dicomConnectionTimeoutDuration(cfg appconfig.Config) time.Duration {
	seconds := timeoutSecondsOrDefault(cfg.DICOMConnectionTimeoutSeconds, appconfig.DefaultDICOMConnectionTimeoutSeconds)
	return time.Duration(seconds) * time.Second
}

func withDICOMCommunicationTimeout(ctx context.Context, state *uiState) (context.Context, context.CancelFunc) {
	cfg := appconfig.Config{}
	if state != nil {
		cfg = state.appConfig
	}
	return context.WithTimeout(ctx, dicomCommunicationTimeoutDuration(cfg))
}

func withDICOMConnectionTimeout(ctx context.Context, state *uiState) (context.Context, context.CancelFunc) {
	cfg := appconfig.Config{}
	if state != nil {
		cfg = state.appConfig
	}
	return context.WithTimeout(ctx, dicomConnectionTimeoutDuration(cfg))
}

func formatOptionalInt64(value *int64) string {
	if value == nil {
		return ""
	}
	return strconv.FormatInt(*value, 10)
}

func formatOptionalInt(value *int) string {
	if value == nil {
		return ""
	}
	return strconv.Itoa(*value)
}

func parseOptionalInt64Limit(value string, name string) (*int64, error) {
	value = strings.TrimSpace(value)
	if value == "" || strings.EqualFold(value, "none") || strings.EqualFold(value, "unlimited") {
		return nil, nil
	}
	parsed, err := strconv.ParseInt(value, 10, 64)
	if err != nil || parsed <= 0 {
		return nil, fmt.Errorf("%s must be a positive integer or blank", name)
	}
	return &parsed, nil
}

func parseOptionalIntLimit(value string, name string) (*int, error) {
	value = strings.TrimSpace(value)
	if value == "" || strings.EqualFold(value, "none") || strings.EqualFold(value, "unlimited") {
		return nil, nil
	}
	parsed, err := strconv.Atoi(value)
	if err != nil || parsed <= 0 {
		return nil, fmt.Errorf("%s must be a positive integer or blank", name)
	}
	return &parsed, nil
}

func parsePositiveSeconds(value string, name string) (int, error) {
	value = strings.TrimSpace(value)
	parsed, err := strconv.Atoi(value)
	if err != nil || parsed <= 0 {
		return 0, fmt.Errorf("%s must be a positive integer number of seconds", name)
	}
	return parsed, nil
}

func parseAETitleList(value string) []string {
	fields := strings.FieldsFunc(value, func(r rune) bool {
		return r == ',' || r == ';' || r == '\n'
	})
	var aeTitles []string
	for _, field := range fields {
		field = strings.TrimSpace(field)
		if field != "" {
			aeTitles = append(aeTitles, field)
		}
	}
	return aeTitles
}

type nodeTLSSettingsState struct {
	SkipVerify bool
	ServerName string
	CAFile     string
	CertFile   string
	KeyFile    string
}

func nodeDraftFromFormState(name string, aeTitle string, host string, port uint16, enabled bool, queryEnabled bool, retrieveMethod string, sendEnabled bool, sendTransferSyntax string, useTLS bool, tlsSettings nodeTLSSettingsState, moveDestination string, notes string) nodes.Draft {
	return nodes.Draft{
		Name:                     name,
		AETitle:                  aeTitle,
		Host:                     host,
		Port:                     port,
		Disabled:                 !enabled,
		QueryDisabled:            !queryEnabled,
		SendDisabled:             !sendEnabled,
		RetrieveMethod:           retrieveMethod,
		SendTransferSyntax:       sendTransferSyntax,
		UseTLS:                   useTLS,
		TLSSkipVerify:            tlsSettings.SkipVerify,
		TLSServerName:            tlsSettings.ServerName,
		TLSCAFile:                tlsSettings.CAFile,
		TLSCertFile:              tlsSettings.CertFile,
		TLSKeyFile:               tlsSettings.KeyFile,
		PreferredMoveDestination: moveDestination,
		Notes:                    notes,
	}
}

func newNodeDialogFormItems(enabled *widget.Check, queryEnabled *widget.Check, retrieveMethod *widget.Select, sendEnabled *widget.Check, tlsControls fyne.CanvasObject, sendSyntax *widget.Select, name *widget.Entry, aeTitle *widget.Entry, host *widget.Entry, port *widget.Entry, moveDestination *widget.Entry, notes *widget.Entry) []*widget.FormItem {
	return []*widget.FormItem{
		widget.NewFormItem("Enabled", enabled),
		widget.NewFormItem("Query", queryEnabled),
		widget.NewFormItem("Retrieve", retrieveMethod),
		widget.NewFormItem("Send", sendEnabled),
		widget.NewFormItem("TLS", tlsControls),
		widget.NewFormItem("Send Syntax", sendSyntax),
		widget.NewFormItem("Name", name),
		widget.NewFormItem("AETitle", aeTitle),
		widget.NewFormItem("Address", host),
		widget.NewFormItem("Port", port),
		widget.NewFormItem("Move Destination", moveDestination),
		widget.NewFormItem("Notes", notes),
	}
}

func retrieveMethodOptions() []string {
	return []string{nodes.RetrieveMethodAuto, nodes.RetrieveMethodMove, nodes.RetrieveMethodGet}
}

func newNodeTLSControls(tls *widget.Check, settingsTapped func()) fyne.CanvasObject {
	if tls == nil {
		tls = widget.NewCheck("Use TLS", nil)
	}
	if settingsTapped == nil {
		settingsTapped = func() {}
	}
	settings := compactToolbarButton("TLS Settings", theme.SettingsIcon(), settingsTapped)
	return container.NewHBox(tls, settings)
}

func showNodeTLSSettingsDialog(w fyne.Window, status *widget.Label, current nodeTLSSettingsState, save func(nodeTLSSettingsState)) {
	skipVerify := widget.NewCheck("Skip certificate verification", nil)
	skipVerify.SetChecked(current.SkipVerify)
	serverName := widget.NewEntry()
	serverName.SetText(current.ServerName)
	caFile := widget.NewEntry()
	caFile.SetText(current.CAFile)
	certFile := widget.NewEntry()
	certFile.SetText(current.CertFile)
	keyFile := widget.NewEntry()
	keyFile.SetText(current.KeyFile)
	form := dialog.NewForm("TLS Settings", "Save", "Cancel", []*widget.FormItem{
		widget.NewFormItem("", skipVerify),
		widget.NewFormItem("Server Name", serverName),
		widget.NewFormItem("CA File", caFile),
		widget.NewFormItem("Client Certificate", certFile),
		widget.NewFormItem("Client Key", keyFile),
	}, func(ok bool) {
		if !ok {
			return
		}
		save(nodeTLSSettingsState{
			SkipVerify: skipVerify.Checked,
			ServerName: strings.TrimSpace(serverName.Text),
			CAFile:     strings.TrimSpace(caFile.Text),
			CertFile:   strings.TrimSpace(certFile.Text),
			KeyFile:    strings.TrimSpace(keyFile.Text),
		})
		if status != nil {
			status.SetText("TLS settings staged")
		}
	}, w)
	form.Resize(fyne.NewSize(600, 300))
	form.Show()
}

func networkNodeActionLabels() []string {
	return []string{
		networkActionLabelAll,
		networkActionLabelNone,
		networkActionLabelSave,
		networkActionLabelLoad,
		networkActionLabelVerify,
		networkActionLabelAddNewNode,
		networkActionLabelEdit,
		networkActionLabelDelete,
	}
}

func networkSecondaryNodeActionButton(icon fyne.Resource, tapped func()) *widget.Button {
	button := widget.NewButtonWithIcon("", icon, tapped)
	button.Importance = widget.LowImportance
	return button
}

func networkDeleteShortcutHint() string {
	return "Press Delete key to remove a node"
}

func networkDeleteShortcutApplies(tabTitle string) bool {
	return tabTitle == networkTabTitle
}

func networkVerifyShortcutApplies(tabTitle string) bool {
	return tabTitle == networkTabTitle
}

func networkVerifyShortcut() *desktop.CustomShortcut {
	return &desktop.CustomShortcut{KeyName: fyne.KeyK, Modifier: fyne.KeyModifierShortcutDefault}
}

func queryShortcutApplies(tabTitle string) bool {
	return tabTitle == "Query"
}

func queryRunShortcut() *desktop.CustomShortcut {
	return &desktop.CustomShortcut{KeyName: fyne.KeyReturn, Modifier: fyne.KeyModifierShortcutDefault}
}

func queryRetrieveShortcut() *desktop.CustomShortcut {
	return &desktop.CustomShortcut{KeyName: fyne.KeyR, Modifier: fyne.KeyModifierShortcutDefault}
}

func queryCancelShortcut() *desktop.CustomShortcut {
	return &desktop.CustomShortcut{KeyName: fyne.KeyEscape}
}

func registerNetworkDeleteShortcut(w fyne.Window, tabs *container.AppTabs, status *widget.Label, nodeTable *widget.Table, state *uiState) {
	if w == nil || tabs == nil {
		return
	}
	deleteShortcut := &desktop.CustomShortcut{KeyName: fyne.KeyDelete}
	w.Canvas().AddShortcut(deleteShortcut, func(fyne.Shortcut) {
		selected := tabs.Selected()
		if selected == nil || !networkDeleteShortcutApplies(selected.Text) {
			return
		}
		deleteSelectedNode(w, status, nodeTable, state)
	})
}

func registerNetworkVerifyShortcut(w fyne.Window, tabs *container.AppTabs, status *widget.Label, nodeTable *widget.Table, state *uiState) {
	if w == nil || tabs == nil {
		return
	}
	w.Canvas().AddShortcut(networkVerifyShortcut(), func(fyne.Shortcut) {
		selected := tabs.Selected()
		if selected == nil || !networkVerifyShortcutApplies(selected.Text) {
			return
		}
		verifySelectedNode(w, status, nodeTable, state)
	})
}

func registerQueryShortcuts(w fyne.Window, tabs *container.AppTabs, status *widget.Label, tables archiveTables, state *uiState) {
	if w == nil || tabs == nil {
		return
	}
	w.Canvas().AddShortcut(queryRunShortcut(), func(fyne.Shortcut) {
		selected := tabs.Selected()
		if selected == nil || !queryShortcutApplies(selected.Text) || state == nil || state.queryRunShortcutAction == nil {
			return
		}
		state.queryRunShortcutAction()
	})
	w.Canvas().AddShortcut(queryRetrieveShortcut(), func(fyne.Shortcut) {
		selected := tabs.Selected()
		if selected == nil || !queryShortcutApplies(selected.Text) {
			return
		}
		retrieveSelectedQuery(w, status, tables, state)
	})
	w.Canvas().AddShortcut(queryCancelShortcut(), func(fyne.Shortcut) {
		selected := tabs.Selected()
		if selected == nil || !queryShortcutApplies(selected.Text) {
			return
		}
		cancelActiveRetrieve(status, state)
	})
}

func nodesJSONData(nodeList []nodes.Node) ([]byte, error) {
	data, err := json.MarshalIndent(nodeList, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("encode nodes export: %w", err)
	}
	return append(data, '\n'), nil
}

func exportNodesToPath(state *uiState, path string) error {
	if state == nil {
		return errors.New("nodes export state is unavailable")
	}
	path = strings.TrimSpace(path)
	if path == "" {
		return errors.New("nodes export path cannot be empty")
	}
	data, err := nodesJSONData(state.nodes)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("create nodes export directory: %w", err)
	}
	if err := os.WriteFile(path, data, 0o644); err != nil {
		return fmt.Errorf("write nodes export: %w", err)
	}
	return nil
}

func importNodesFromPath(state *uiState, path string) error {
	path = strings.TrimSpace(path)
	if path == "" {
		return errors.New("nodes import path cannot be empty")
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("read nodes import: %w", err)
	}
	return importNodesFromJSON(state, data)
}

func importNodesFromJSON(state *uiState, data []byte) error {
	if state == nil {
		return errors.New("nodes import state is unavailable")
	}
	var imported []nodes.Node
	if err := json.Unmarshal(data, &imported); err != nil {
		return fmt.Errorf("parse nodes import: %w", err)
	}
	originalNodes := append([]nodes.Node(nil), state.nodes...)
	originalRows := append([]int(nil), state.nodeTableRows...)
	originalSelection := state.selectedNodeRow
	state.nodes = append([]nodes.Node(nil), imported...)
	state.selectedNodeRow = -1
	applySavedNodeSortPreference(state)
	if state.nodeStore != nil {
		if err := state.nodeStore.Save(state.nodes); err != nil {
			state.nodes = originalNodes
			state.nodeTableRows = originalRows
			state.selectedNodeRow = originalSelection
			return fmt.Errorf("persist nodes import: %w", err)
		}
	}
	return nil
}

func showSaveNodesDialog(w fyne.Window, status *widget.Label, state *uiState) {
	picker := dialog.NewFileSave(func(writer fyne.URIWriteCloser, err error) {
		if err != nil {
			if status != nil {
				status.SetText("Node export failed")
			}
			dialog.ShowError(err, w)
			return
		}
		if writer == nil {
			return
		}
		defer writer.Close()
		data, err := nodesJSONData(state.nodes)
		if err != nil {
			if status != nil {
				status.SetText("Node export failed")
			}
			dialog.ShowError(err, w)
			return
		}
		if _, err := writer.Write(data); err != nil {
			if status != nil {
				status.SetText("Node export failed")
			}
			dialog.ShowError(err, w)
			return
		}
		if status != nil {
			status.SetText(fmt.Sprintf("Exported %d nodes to %s", len(state.nodes), writer.URI().Name()))
		}
	}, w)
	picker.SetFileName("go-pacs-nodes.json")
	picker.Show()
}

func showLoadNodesDialog(w fyne.Window, status *widget.Label, table *widget.Table, state *uiState) {
	picker := dialog.NewFileOpen(func(reader fyne.URIReadCloser, err error) {
		if err != nil {
			if status != nil {
				status.SetText("Node import failed")
			}
			dialog.ShowError(err, w)
			return
		}
		if reader == nil {
			return
		}
		defer reader.Close()
		data, err := io.ReadAll(reader)
		if err != nil {
			if status != nil {
				status.SetText("Node import failed")
			}
			dialog.ShowError(err, w)
			return
		}
		if err := importNodesFromJSON(state, data); err != nil {
			if status != nil {
				status.SetText("Node import failed")
			}
			dialog.ShowError(err, w)
			return
		}
		refreshArchiveChrome(state)
		refreshQueryDestination(state)
		refreshQueryResultSummary(state)
		refreshQuerySourceList(state)
		if table != nil {
			table.Refresh()
		}
		if status != nil {
			status.SetText(fmt.Sprintf("Imported %d nodes from %s", len(state.nodes), reader.URI().Name()))
		}
	}, w)
	picker.Show()
}

func newNetworkHeader() fyne.CanvasObject {
	title := widget.NewLabelWithStyle("DICOM Nodes for DICOM Query/Retrieve and DICOM Send", fyne.TextAlignLeading, fyne.TextStyle{Bold: true})
	hint := widget.NewLabelWithStyle(networkDeleteShortcutHint(), fyne.TextAlignTrailing, fyne.TextStyle{})
	hint.Alignment = fyne.TextAlignTrailing
	return workbenchStrip(container.NewBorder(nil, nil, title, hint))
}

func newNetworkFooter(left fyne.CanvasObject, center fyne.CanvasObject, right fyne.CanvasObject) fyne.CanvasObject {
	return workbenchStrip(container.NewBorder(nil, nil, left, right, container.NewCenter(center)))
}

const (
	networkFooterBulkButtonSlotWidth   float32 = 86
	networkFooterActionButtonSlotWidth float32 = 104
	networkFooterAddButtonSlotWidth    float32 = 204
	networkFooterIconButtonSlotWidth   float32 = 44
)

func networkFooterButtonSlot(button *widget.Button, width float32) fyne.CanvasObject {
	if button == nil {
		return container.NewGridWrap(fyne.NewSize(width, 1), canvas.NewRectangle(color.Transparent))
	}
	return container.NewGridWrap(fyne.NewSize(width, button.MinSize().Height), button)
}

func networkFooterIconButtonSlot(button *widget.Button) fyne.CanvasObject {
	return networkFooterButtonSlot(button, networkFooterIconButtonSlotWidth)
}

func networkAddNodeButton(tapped func()) *widget.Button {
	return widget.NewButton(networkActionLabelAddNewNode, tapped)
}

func networkFooterActionButton(label string, tapped func()) *widget.Button {
	return widget.NewButton(label, tapped)
}

func newNetworkTab(w fyne.Window, status *widget.Label, nodeTable *widget.Table, state *uiState) fyne.CanvasObject {
	header := newNetworkHeader()

	refreshAfterBulk := func(changed bool, err error, action string) {
		if err != nil {
			if status != nil {
				status.SetText("Node update failed")
			}
			dialog.ShowError(err, w)
			return
		}
		if changed && status != nil {
			status.SetText(action)
		}
		refreshArchiveChrome(state)
		refreshQueryDestination(state)
		refreshQueryResultSummary(state)
		refreshQuerySourceList(state)
		if nodeTable != nil {
			nodeTable.Refresh()
		}
	}
	allButton := widget.NewButton(networkActionLabelAll, func() {
		changed, err := setAllNodesEnabled(state, true)
		refreshAfterBulk(changed, err, "Enabled all nodes")
	})
	noneButton := widget.NewButton(networkActionLabelNone, func() {
		changed, err := setAllNodesEnabled(state, false)
		refreshAfterBulk(changed, err, "Disabled all nodes")
	})
	saveButton := networkFooterActionButton(networkActionLabelSave, func() {
		showSaveNodesDialog(w, status, state)
	})
	loadButton := networkFooterActionButton(networkActionLabelLoad, func() {
		showLoadNodesDialog(w, status, nodeTable, state)
	})
	verifyButton := networkFooterActionButton(networkActionLabelVerify, func() {
		verifySelectedNode(w, status, nodeTable, state)
	})
	addButton := networkAddNodeButton(func() {
		showAddNodeDialog(w, status, nodeTable, state)
	})
	editButton := networkSecondaryNodeActionButton(theme.DocumentCreateIcon(), func() {
		showEditNodeDialog(w, status, nodeTable, state)
	})
	deleteButton := networkSecondaryNodeActionButton(theme.DeleteIcon(), func() {
		deleteSelectedNode(w, status, nodeTable, state)
	})
	footer := newNetworkFooter(
		container.NewHBox(
			networkFooterButtonSlot(allButton, networkFooterBulkButtonSlotWidth),
			networkFooterButtonSlot(noneButton, networkFooterBulkButtonSlotWidth),
		),
		container.NewHBox(
			networkFooterButtonSlot(saveButton, networkFooterActionButtonSlotWidth),
			networkFooterButtonSlot(loadButton, networkFooterActionButtonSlotWidth),
			networkFooterButtonSlot(verifyButton, networkFooterActionButtonSlotWidth),
		),
		container.NewHBox(
			networkFooterIconButtonSlot(editButton),
			networkFooterIconButtonSlot(deleteButton),
			networkFooterButtonSlot(addButton, networkFooterAddButtonSlotWidth),
		),
	)
	return container.NewBorder(header, footer, nil, nil, container.NewStack(nodeTable))
}

func showAddNodeDialog(w fyne.Window, status *widget.Label, table *widget.Table, state *uiState) {
	enabled := widget.NewCheck("", nil)
	enabled.SetChecked(true)
	queryEnabled := widget.NewCheck("", nil)
	queryEnabled.SetChecked(true)
	retrieveMethod := widget.NewSelect(retrieveMethodOptions(), nil)
	retrieveMethod.SetSelected(nodes.RetrieveMethodAuto)
	sendEnabled := widget.NewCheck("", nil)
	sendEnabled.SetChecked(true)
	sendSyntax := widget.NewSelect(sendSyntaxOptions(), nil)
	sendSyntax.SetSelected(sendSyntaxAutoLabel)
	useTLS := widget.NewCheck("Use TLS", nil)
	tlsSettings := nodeTLSSettingsState{}
	tlsControls := newNodeTLSControls(useTLS, func() {
		showNodeTLSSettingsDialog(w, status, tlsSettings, func(updated nodeTLSSettingsState) {
			tlsSettings = updated
		})
	})
	name := widget.NewEntry()
	name.SetPlaceHolder("pacs")
	aeTitle := widget.NewEntry()
	aeTitle.SetPlaceHolder("REMOTEAE")
	host := widget.NewEntry()
	host.SetPlaceHolder("127.0.0.1")
	port := widget.NewEntry()
	port.SetPlaceHolder("104")
	moveDestination := widget.NewEntry()
	moveDestination.SetPlaceHolder(localAETitle(state))
	notes := widget.NewMultiLineEntry()
	notes.SetPlaceHolder("Optional notes")

	form := dialog.NewForm("Add Remote Node", "Add", "Cancel", newNodeDialogFormItems(enabled, queryEnabled, retrieveMethod, sendEnabled, tlsControls, sendSyntax, name, aeTitle, host, port, moveDestination, notes), func(ok bool) {
		if !ok {
			return
		}
		portValue, err := parsePort(port.Text)
		if err != nil {
			status.SetText("Add node failed")
			dialog.ShowError(err, w)
			return
		}
		node, err := state.nodeStore.Add(nodeDraftFromFormState(name.Text, aeTitle.Text, host.Text, portValue, enabled.Checked, queryEnabled.Checked, retrieveMethod.Selected, sendEnabled.Checked, sendSyntaxValue(sendSyntax.Selected), useTLS.Checked, tlsSettings, moveDestination.Text, notes.Text))
		if err != nil {
			status.SetText("Add node failed")
			dialog.ShowError(err, w)
			return
		}
		state.nodes = append(state.nodes, node)
		refreshNodeTableRows(state)
		table.Refresh()
		refreshArchiveChrome(state)
		refreshQueryDestination(state)
		refreshQueryResultSummary(state)
		refreshQuerySourceList(state)
		status.SetText("Added node " + node.Name)
	}, w)
	form.Resize(fyne.NewSize(560, 460))
	form.Show()
}

func showEditNodeDialog(w fyne.Window, status *widget.Label, table *widget.Table, state *uiState) {
	node, ok := selectedNode(state)
	if !ok {
		status.SetText("Select a remote node to edit")
		return
	}
	row := state.selectedNodeRow
	enabled := widget.NewCheck("", nil)
	enabled.SetChecked(node.Enabled())
	queryEnabled := widget.NewCheck("", nil)
	queryEnabled.SetChecked(node.QueryEnabled())
	retrieveMethod := widget.NewSelect(retrieveMethodOptions(), nil)
	retrieveMethod.SetSelected(node.RetrieveMethodOrDefault())
	sendEnabled := widget.NewCheck("", nil)
	sendEnabled.SetChecked(node.SendEnabled())
	sendSyntax := widget.NewSelect(sendSyntaxOptions(), nil)
	sendSyntax.SetSelected(sendSyntaxLabel(node.SendTransferSyntaxOrDefault()))
	useTLS := widget.NewCheck("Use TLS", nil)
	useTLS.SetChecked(node.UseTLS)
	tlsSettings := nodeTLSSettingsState{
		SkipVerify: node.TLSSkipVerify,
		ServerName: node.TLSServerName,
		CAFile:     node.TLSCAFile,
		CertFile:   node.TLSCertFile,
		KeyFile:    node.TLSKeyFile,
	}
	tlsControls := newNodeTLSControls(useTLS, func() {
		showNodeTLSSettingsDialog(w, status, tlsSettings, func(updated nodeTLSSettingsState) {
			tlsSettings = updated
		})
	})
	name := widget.NewEntry()
	name.SetText(node.Name)
	aeTitle := widget.NewEntry()
	aeTitle.SetText(node.AETitle)
	host := widget.NewEntry()
	host.SetText(node.Host)
	port := widget.NewEntry()
	port.SetText(strconv.Itoa(int(node.Port)))
	moveDestination := widget.NewEntry()
	moveDestination.SetText(node.PreferredMoveDestination)
	notes := widget.NewMultiLineEntry()
	notes.SetText(node.Notes)

	form := dialog.NewForm("Edit Remote Node", "Save", "Cancel", newNodeDialogFormItems(enabled, queryEnabled, retrieveMethod, sendEnabled, tlsControls, sendSyntax, name, aeTitle, host, port, moveDestination, notes), func(ok bool) {
		if !ok {
			return
		}
		portValue, err := parsePort(port.Text)
		if err != nil {
			status.SetText("Edit node failed")
			dialog.ShowError(err, w)
			return
		}
		updated, err := state.nodeStore.Update(node.ID, nodeDraftFromFormState(name.Text, aeTitle.Text, host.Text, portValue, enabled.Checked, queryEnabled.Checked, retrieveMethod.Selected, sendEnabled.Checked, sendSyntaxValue(sendSyntax.Selected), useTLS.Checked, tlsSettings, moveDestination.Text, notes.Text))
		if err != nil {
			status.SetText("Edit node failed")
			dialog.ShowError(err, w)
			return
		}
		if row >= 0 && row < len(state.nodes) && state.nodes[row].ID == updated.ID {
			state.nodes[row] = updated
		} else {
			nodeList, err := state.nodeStore.List()
			if err != nil {
				status.SetText("Edit node saved, refresh failed")
				dialog.ShowError(err, w)
				return
			}
			state.nodes = nodeList
			state.selectedNodeRow = -1
		}
		refreshNodeTableRows(state)
		table.Refresh()
		refreshArchiveChrome(state)
		refreshQueryDestination(state)
		refreshQueryResultSummary(state)
		refreshQuerySourceList(state)
		status.SetText("Edited node " + updated.Name)
	}, w)
	form.Resize(fyne.NewSize(560, 460))
	form.Show()
}

func deleteSelectedNode(w fyne.Window, status *widget.Label, table *widget.Table, state *uiState) {
	node, ok := selectedNode(state)
	if !ok {
		status.SetText("Select a remote node to delete")
		return
	}
	dialog.ShowConfirm("Delete Remote Node", fmt.Sprintf("Delete %s?", node.Name), func(ok bool) {
		if !ok {
			return
		}
		if err := state.nodeStore.Delete(node.ID); err != nil {
			status.SetText("Delete node failed")
			dialog.ShowError(err, w)
			return
		}
		row := state.selectedNodeRow
		if row >= 0 && row < len(state.nodes) && state.nodes[row].ID == node.ID {
			state.nodes = append(state.nodes[:row], state.nodes[row+1:]...)
		} else {
			nodeList, err := state.nodeStore.List()
			if err != nil {
				status.SetText("Delete node saved, refresh failed")
				dialog.ShowError(err, w)
				return
			}
			state.nodes = nodeList
		}
		state.selectedNodeRow = -1
		refreshNodeTableRows(state)
		table.Refresh()
		refreshArchiveChrome(state)
		refreshQueryDestination(state)
		refreshQueryResultSummary(state)
		refreshQuerySourceList(state)
		status.SetText("Deleted node " + node.Name)
	}, w)
}

func parsePort(value string) (uint16, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return 0, fmt.Errorf("port is required")
	}
	port, err := strconv.Atoi(value)
	if err != nil {
		return 0, fmt.Errorf("invalid port %q", value)
	}
	if port < 1 || port > 65535 {
		return 0, fmt.Errorf("port must be between 1 and 65535")
	}
	return uint16(port), nil
}

func verifySelectedNode(w fyne.Window, status *widget.Label, table *widget.Table, state *uiState) {
	node, ok := selectedEnabledNode(state)
	if !ok {
		status.SetText("No enabled remote nodes configured")
		return
	}
	callingAE := localAETitle(state)
	status.SetText("Verifying " + node.Name)
	go func() {
		ctx, cancel := withDICOMConnectionTimeout(context.Background(), state)
		defer cancel()
		result, err := netverify.Echo(ctx, node, callingAE)
		fyne.Do(func() {
			if err != nil {
				recordNodeVerifyStatus(state, node, nodeVerifyFail)
				status.SetText("C-ECHO failed for " + node.Name)
				refreshQuerySourceList(state)
				dialog.ShowError(err, w)
				return
			}
			recordNodeVerifyStatus(state, node, nodeVerifyOK)
			status.SetText(fmt.Sprintf("C-ECHO %s status=0x%04X in %s", result.NodeName, result.Status, result.Duration.Round(time.Millisecond)))
			refreshQuerySourceList(state)
			if table != nil {
				table.Refresh()
			}
		})
	}()
}

func startReceiver(w fyne.Window, status *widget.Label, state *uiState) {
	if state.receiver != nil {
		snapshot := state.receiver.Snapshot()
		status.SetText(fmt.Sprintf("Receiver already listening on %s as %s", snapshot.Address, snapshot.AETitle))
		return
	}
	allowedCallingAEs := configuredNodeAETitles(state.nodes)
	remoteAllowlist := nodes.RemoteHostAllowlist(state.nodes)
	allowedRemoteHosts := remoteAllowlist.Hosts
	tlsConfig, err := receiverTLSConfig(state.appConfig)
	if err != nil {
		status.SetText("Receiver start failed")
		dialog.ShowError(err, w)
		return
	}
	server, err := receive.Start(context.Background(), receive.Config{
		Catalog:                 state.catalog,
		Address:                 state.appConfig.ReceiverAddress,
		AETitle:                 localAETitle(state),
		AllowedCalledAETitles:   state.appConfig.AdditionalAETitles,
		AllowedCallingAETitles:  allowedCallingAEs,
		AllowedRemoteHosts:      allowedRemoteHosts,
		MaxStoreObjectBytes:     optionalInt64Value(state.appConfig.MaxStoreObjectBytes),
		PreferredTransferSyntax: state.appConfig.ReceivePreferredTransferSyntax,
		DecompressImages:        state.appConfig.ReceiveDecompressImages,
		TLSConfig:               tlsConfig,
	})
	if err != nil {
		status.SetText("Receiver start failed")
		dialog.ShowError(err, w)
		return
	}
	state.receiver = server
	state.receiverStartedAt = time.Now()
	refreshArchiveChrome(state)
	refreshQueryDestination(state)
	refreshQueryResultSummary(state)
	refreshQuerySourceList(state)
	if len(remoteAllowlist.Warnings) > 0 {
		dialog.ShowInformation("Receiver allowlist warnings", strings.Join(remoteAllowlist.Warnings, "\n"), w)
	}
	if len(allowedCallingAEs) > 0 {
		message := fmt.Sprintf("Receiver listening on %s as %s; allowing %d remote AEs and %d remote addresses", server.Addr(), server.AETitle(), len(allowedCallingAEs), len(allowedRemoteHosts))
		if len(remoteAllowlist.Warnings) > 0 {
			message += fmt.Sprintf("; skipped %d remote hostnames", len(remoteAllowlist.Warnings))
		}
		status.SetText(message)
		return
	}
	status.SetText(fmt.Sprintf("Receiver listening on %s as %s; no Calling AE allowlist", server.Addr(), server.AETitle()))
}

func receiverTLSConfig(cfg appconfig.Config) (*tls.Config, error) {
	if !cfg.ReceiverUseTLS {
		return nil, nil
	}
	cert, err := tls.LoadX509KeyPair(cfg.ReceiverTLSCertFile, cfg.ReceiverTLSKeyFile)
	if err != nil {
		return nil, fmt.Errorf("load receiver TLS certificate: %w", err)
	}
	return &tls.Config{
		MinVersion:   tls.VersionTLS12,
		Certificates: []tls.Certificate{cert},
	}, nil
}

func configuredNodeAETitles(nodeList []nodes.Node) []string {
	seen := map[string]bool{}
	var aeTitles []string
	for _, node := range nodeList {
		aeTitle := nodes.NormalizeAETitle(node.AETitle)
		if aeTitle == "" || seen[aeTitle] {
			continue
		}
		seen[aeTitle] = true
		aeTitles = append(aeTitles, aeTitle)
	}
	return aeTitles
}

func stopReceiver(w fyne.Window, status *widget.Label, tables archiveTables, state *uiState) {
	if state.receiver == nil {
		status.SetText("Receiver is not running")
		return
	}
	server := state.receiver
	status.SetText("Stopping receiver")
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		err := server.Stop(ctx)
		studies, studyErr := loadStudies(context.Background(), state)
		fyne.Do(func() {
			if err != nil {
				status.SetText("Receiver stop failed")
				dialog.ShowError(err, w)
				return
			}
			state.receiver = nil
			refreshQueryDestination(state)
			refreshQueryResultSummary(state)
			refreshQuerySourceList(state)
			snapshot := server.Snapshot()
			duration := time.Duration(0)
			if !state.receiverStartedAt.IsZero() {
				duration = time.Since(state.receiverStartedAt)
			}
			state.receiverStartedAt = time.Time{}
			recordOperation(state, ops.ReceiverSummary(snapshot, duration))
			if studyErr == nil {
				setStudies(state, tables, studies)
			}
			status.SetText(fmt.Sprintf("Receiver stopped: stored %d, duplicates %d, rejected %d, failed %d", snapshot.Stored, snapshot.Duplicates, snapshot.Rejected, snapshot.Failed))
			if studyErr != nil {
				dialog.ShowError(studyErr, w)
			}
		})
	}()
}

func sendSelectedStudy(w fyne.Window, status *widget.Label, state *uiState) {
	study, ok := selectedStudy(state)
	if !ok {
		status.SetText("Select a study to send")
		return
	}
	if strings.TrimSpace(study.StudyInstanceUID) == "" || study.StudyInstanceUID == "(missing)" {
		status.SetText("Selected study has no Study Instance UID")
		return
	}
	node, ok := selectedSendNode(state)
	if !ok {
		status.SetText("No send-enabled remote nodes configured")
		return
	}
	callingAE := localAETitle(state)
	status.SetText(fmt.Sprintf("Sending study %s to %s", study.StudyInstanceUID, node.Name))
	beginSendActivity(state, "Study C-STORE "+node.Name)
	go func() {
		ctx, cancel := withDICOMCommunicationTimeout(context.Background(), state)
		defer cancel()
		outcome, err := send.SendStudyWithOptions(ctx, state.catalog, node, study.StudyInstanceUID, send.Options{
			CallingAETitle: callingAE,
			OnProgress:     sendProgressCallback(state),
		})
		fyne.Do(func() {
			clearActiveSendActivity(state)
			if err != nil {
				status.SetText("C-STORE failed for " + node.Name)
				dialog.ShowError(err, w)
				return
			}
			recordOperation(state, ops.SendSummary(outcome))
			status.SetText(fmt.Sprintf(
				"C-STORE %s: attempted %d, sent %d, warnings %d, failed %d in %s",
				node.Name,
				outcome.Attempted,
				outcome.Sent,
				outcome.Warnings,
				outcome.Failed,
				outcome.Duration.Round(time.Millisecond),
			))
			if outcome.Failed > 0 {
				dialog.ShowInformation("Send completed with failures", strings.Join(outcome.Failures, "\n"), w)
			}
		})
	}()
}

func sendSelectedSeries(w fyne.Window, status *widget.Label, state *uiState) {
	series, ok := selectedSeries(state)
	if !ok {
		status.SetText("Select a series to send")
		return
	}
	if strings.TrimSpace(series.SeriesInstanceUID) == "" || series.SeriesInstanceUID == "(missing)" {
		status.SetText("Selected series has no Series Instance UID")
		return
	}
	node, ok := selectedSendNode(state)
	if !ok {
		status.SetText("No send-enabled remote nodes configured")
		return
	}
	callingAE := localAETitle(state)
	status.SetText(fmt.Sprintf("Sending series %s to %s", series.SeriesInstanceUID, node.Name))
	beginSendActivity(state, "Series C-STORE "+node.Name)
	go func() {
		ctx, cancel := withDICOMCommunicationTimeout(context.Background(), state)
		defer cancel()
		outcome, err := send.SendSeriesWithOptions(ctx, state.catalog, node, series.SeriesInstanceUID, send.Options{
			CallingAETitle: callingAE,
			OnProgress:     sendProgressCallback(state),
		})
		fyne.Do(func() {
			clearActiveSendActivity(state)
			if err != nil {
				status.SetText("C-STORE failed for " + node.Name)
				dialog.ShowError(err, w)
				return
			}
			recordOperation(state, ops.SendSummary(outcome))
			status.SetText(fmt.Sprintf(
				"C-STORE %s: attempted %d, sent %d, warnings %d, failed %d in %s",
				node.Name,
				outcome.Attempted,
				outcome.Sent,
				outcome.Warnings,
				outcome.Failed,
				outcome.Duration.Round(time.Millisecond),
			))
			if outcome.Failed > 0 {
				dialog.ShowInformation("Send completed with failures", strings.Join(outcome.Failures, "\n"), w)
			}
		})
	}()
}

func sendSelectedInstance(w fyne.Window, status *widget.Label, state *uiState) {
	instance, ok := selectedInstance(state)
	if !ok {
		status.SetText("Select an image to send")
		return
	}
	if strings.TrimSpace(instance.SOPInstanceUID) == "" || instance.SOPInstanceUID == "(missing)" {
		status.SetText("Selected image has no SOP Instance UID")
		return
	}
	node, ok := selectedSendNode(state)
	if !ok {
		status.SetText("No send-enabled remote nodes configured")
		return
	}
	callingAE := localAETitle(state)
	status.SetText(fmt.Sprintf("Sending image %s to %s", instance.SOPInstanceUID, node.Name))
	beginSendActivity(state, "Image C-STORE "+node.Name)
	go func() {
		ctx, cancel := withDICOMCommunicationTimeout(context.Background(), state)
		defer cancel()
		outcome, err := send.SendInstanceWithOptions(ctx, state.catalog, node, instance.SOPInstanceUID, send.Options{
			CallingAETitle: callingAE,
			OnProgress:     sendProgressCallback(state),
		})
		fyne.Do(func() {
			clearActiveSendActivity(state)
			if err != nil {
				status.SetText("C-STORE failed for " + node.Name)
				dialog.ShowError(err, w)
				return
			}
			recordOperation(state, ops.SendSummary(outcome))
			status.SetText(fmt.Sprintf(
				"C-STORE %s: attempted %d, sent %d, warnings %d, failed %d in %s",
				node.Name,
				outcome.Attempted,
				outcome.Sent,
				outcome.Warnings,
				outcome.Failed,
				outcome.Duration.Round(time.Millisecond),
			))
			if outcome.Failed > 0 {
				dialog.ShowInformation("Send completed with failures", strings.Join(outcome.Failures, "\n"), w)
			}
		})
	}()
}

func retrieveSelectedSeries(w fyne.Window, status *widget.Label, tables archiveTables, state *uiState) {
	series, ok := selectedSeries(state)
	if !ok {
		status.SetText("Select a series to retrieve")
		return
	}
	if strings.TrimSpace(series.StudyInstanceUID) == "" || series.StudyInstanceUID == "(missing)" {
		status.SetText("Selected series has no Study Instance UID")
		return
	}
	if strings.TrimSpace(series.SeriesInstanceUID) == "" || series.SeriesInstanceUID == "(missing)" {
		status.SetText("Selected series has no Series Instance UID")
		return
	}
	node, ok := selectedQueryNode(state)
	if !ok {
		status.SetText("No query-enabled remote nodes configured")
		return
	}
	if issue := retrieveReceiverAddressIssue(state, node); issue != "" {
		status.SetText(issue)
		return
	}
	opts := retrieveOptionsForNode(status, state, node)
	study, _ := selectedStudy(state)
	baseCtx, cancel := beginRetrieve(state, node.Name, archiveSeriesRetrieveActivityLabel(study, series))
	ctx, timeoutCancel := withDICOMCommunicationTimeout(baseCtx, state)
	status.SetText(fmt.Sprintf("Retrieving series %s from %s", series.SeriesInstanceUID, node.Name))
	go func() {
		defer timeoutCancel()
		defer cancel()
		outcome, err := retrieve.RetrieveSeries(ctx, state.catalog, node, series.StudyInstanceUID, series.SeriesInstanceUID, opts)
		studies, studyErr := loadStudies(context.Background(), state)
		fyne.Do(func() {
			clearActiveRetrieve(state)
			if err != nil {
				if errors.Is(err, context.Canceled) {
					status.SetText("Retrieve cancelled for " + node.Name)
					return
				}
				status.SetText("Retrieve failed for " + node.Name)
				dialog.ShowError(err, w)
				return
			}
			if studyErr == nil {
				setStudies(state, tables, studies)
			}
			recordOperation(state, ops.RetrieveSummary(outcome))
			status.SetText(fmt.Sprintf(
				"%s %s: final=0x%04X completed %d failed %d warnings %d stored %d in %s",
				retrieveMethodName(outcome),
				node.Name,
				outcome.FinalStatus,
				outcome.Completed,
				outcome.Failed,
				outcome.Warnings,
				outcome.Stored,
				outcome.Duration.Round(time.Millisecond),
			))
			if studyErr != nil {
				dialog.ShowError(studyErr, w)
			}
		})
	}()
}

func retrieveSelectedInstance(w fyne.Window, status *widget.Label, tables archiveTables, state *uiState) {
	instance, ok := selectedInstance(state)
	if !ok {
		status.SetText("Select an image to retrieve")
		return
	}
	if strings.TrimSpace(instance.StudyInstanceUID) == "" || instance.StudyInstanceUID == "(missing)" {
		status.SetText("Selected image has no Study Instance UID")
		return
	}
	if strings.TrimSpace(instance.SeriesInstanceUID) == "" || instance.SeriesInstanceUID == "(missing)" {
		status.SetText("Selected image has no Series Instance UID")
		return
	}
	if strings.TrimSpace(instance.SOPInstanceUID) == "" || instance.SOPInstanceUID == "(missing)" {
		status.SetText("Selected image has no SOP Instance UID")
		return
	}
	node, ok := selectedQueryNode(state)
	if !ok {
		status.SetText("No query-enabled remote nodes configured")
		return
	}
	if issue := retrieveReceiverAddressIssue(state, node); issue != "" {
		status.SetText(issue)
		return
	}
	opts := retrieveOptionsForNode(status, state, node)
	study, _ := selectedStudy(state)
	baseCtx, cancel := beginRetrieve(state, node.Name, archiveImageRetrieveActivityLabel(study, instance))
	ctx, timeoutCancel := withDICOMCommunicationTimeout(baseCtx, state)
	status.SetText(fmt.Sprintf("Retrieving image %s from %s", instance.SOPInstanceUID, node.Name))
	go func() {
		defer timeoutCancel()
		defer cancel()
		outcome, err := retrieve.RetrieveImage(ctx, state.catalog, node, instance.StudyInstanceUID, instance.SeriesInstanceUID, instance.SOPInstanceUID, opts)
		studies, studyErr := loadStudies(context.Background(), state)
		fyne.Do(func() {
			clearActiveRetrieve(state)
			if err != nil {
				if errors.Is(err, context.Canceled) {
					status.SetText("Retrieve cancelled for " + node.Name)
					return
				}
				status.SetText("Retrieve failed for " + node.Name)
				dialog.ShowError(err, w)
				return
			}
			if studyErr == nil {
				setStudies(state, tables, studies)
			}
			recordOperation(state, ops.RetrieveSummary(outcome))
			status.SetText(fmt.Sprintf(
				"%s %s: final=0x%04X completed %d failed %d warnings %d stored %d in %s",
				retrieveMethodName(outcome),
				node.Name,
				outcome.FinalStatus,
				outcome.Completed,
				outcome.Failed,
				outcome.Warnings,
				outcome.Stored,
				outcome.Duration.Round(time.Millisecond),
			))
			if studyErr != nil {
				dialog.ShowError(studyErr, w)
			}
		})
	}()
}

type archiveRowKind int

const (
	archiveRowPatient archiveRowKind = iota
	archiveRowStudy
	archiveRowSeries
	archiveRowInstance
)

type archiveBrowserRow struct {
	kind               archiveRowKind
	studyIndex         int
	seriesIndex        int
	instanceIndex      int
	groupKey           string
	collapsed          bool
	studyHasSeries     bool
	studySeriesLoaded  bool
	seriesHasImages    bool
	seriesImagesLoaded bool
	series             archive.Series
	instance           archive.Instance
	patientName        string
	patientID          string
	patientBirthDate   string
	institutionName    string
	modalities         string
	studyDate          string
	studyTime          string
	importedAt         time.Time
	seriesCount        int
	instanceCount      int
}

type archivePatientGroup struct {
	row          archiveBrowserRow
	studyIndexes []int
}

func archiveBrowserRows(studies []archive.Study) []archiveBrowserRow {
	return archiveBrowserRowsWithCollapse(studies, nil)
}

func archiveBrowserRowsWithCollapse(studies []archive.Study, collapsed map[string]bool) []archiveBrowserRow {
	return archiveBrowserRowsWithInlineSeries(studies, collapsed, nil)
}

func archiveBrowserRowsWithInlineSeries(studies []archive.Study, collapsed map[string]bool, seriesByStudy map[string][]archive.Series) []archiveBrowserRow {
	return archiveBrowserRowsWithInlineSeriesAndInstances(studies, collapsed, seriesByStudy, nil)
}

func archiveBrowserRowsWithInlineSeriesAndCollapsedStudies(studies []archive.Study, collapsed map[string]bool, seriesByStudy map[string][]archive.Series, collapsedStudies map[string]bool) []archiveBrowserRow {
	return archiveBrowserRowsWithInlineSeriesInstancesAndCollapsedStudies(studies, collapsed, seriesByStudy, nil, collapsedStudies)
}

func archiveBrowserRowsWithInlineSeriesAndInstances(studies []archive.Study, collapsed map[string]bool, seriesByStudy map[string][]archive.Series, instancesBySeries map[string][]archive.Instance) []archiveBrowserRow {
	return archiveBrowserRowsWithInlineSeriesInstancesAndCollapsedStudies(studies, collapsed, seriesByStudy, instancesBySeries, nil)
}

func archiveBrowserRowsWithInlineSeriesInstancesAndCollapsedStudies(studies []archive.Study, collapsed map[string]bool, seriesByStudy map[string][]archive.Series, instancesBySeries map[string][]archive.Instance, collapsedStudies map[string]bool) []archiveBrowserRow {
	return archiveBrowserRowsWithInlineSeriesInstancesAndCollapsed(studies, collapsed, seriesByStudy, instancesBySeries, collapsedStudies, nil)
}

func archiveBrowserRowsWithInlineSeriesInstancesAndCollapsed(studies []archive.Study, collapsed map[string]bool, seriesByStudy map[string][]archive.Series, instancesBySeries map[string][]archive.Instance, collapsedStudies map[string]bool, collapsedSeries map[string]bool) []archiveBrowserRow {
	groupIndex := map[string]int{}
	var groups []archivePatientGroup
	for index, study := range studies {
		key := archivePatientKey(study)
		groupIndexValue, ok := groupIndex[key]
		if !ok {
			groupIndexValue = len(groups)
			groupIndex[key] = groupIndexValue
			groups = append(groups, archivePatientGroup{row: archiveBrowserRow{
				kind:        archiveRowPatient,
				studyIndex:  -1,
				groupKey:    key,
				collapsed:   collapsed[key],
				patientName: emptyDash(displayPatientName(study.PatientName)),
				patientID:   emptyDash(study.PatientID),
			}})
		}
		group := &groups[groupIndexValue]
		if group.row.patientBirthDate == "" {
			group.row.patientBirthDate = study.PatientBirthDate
		}
		if group.row.institutionName == "" {
			group.row.institutionName = study.InstitutionName
		}
		group.row.seriesCount += study.SeriesCount
		group.row.instanceCount += study.InstanceCount
		group.row.modalities = mergeModalities(group.row.modalities, study.Modalities)
		// Surface the most recent acquisition/import on the collapsed patient row
		// so the exam list shows Date Acquired / Date Added without expanding.
		if strings.TrimSpace(study.StudyDate) > strings.TrimSpace(group.row.studyDate) {
			group.row.studyDate = study.StudyDate
			group.row.studyTime = study.StudyTime
		}
		if study.ImportedAt.After(group.row.importedAt) {
			group.row.importedAt = study.ImportedAt
		}
		group.studyIndexes = append(group.studyIndexes, index)
	}

	var rows []archiveBrowserRow
	for _, group := range groups {
		rows = append(rows, group.row)
		if group.row.collapsed {
			continue
		}
		for _, studyIndex := range group.studyIndexes {
			if studyIndex < 0 || studyIndex >= len(studies) {
				rows = append(rows, archiveBrowserRow{kind: archiveRowStudy, studyIndex: studyIndex})
				continue
			}
			studyUID := studies[studyIndex].StudyInstanceUID
			seriesRows := seriesByStudy[studyUID]
			studySeriesCollapsed := collapsedStudies[studyUID]
			rows = append(rows, archiveBrowserRow{
				kind:              archiveRowStudy,
				studyIndex:        studyIndex,
				studyHasSeries:    studies[studyIndex].SeriesCount > 0 || len(seriesRows) > 0,
				studySeriesLoaded: len(seriesRows) > 0 && !studySeriesCollapsed,
			})
			if studySeriesCollapsed {
				continue
			}
			for seriesIndex, series := range seriesRows {
				seriesUID := strings.TrimSpace(series.SeriesInstanceUID)
				instanceRows := instancesBySeries[seriesUID]
				seriesImagesCollapsed := collapsedSeries[seriesUID]
				rows = append(rows, archiveBrowserRow{
					kind:               archiveRowSeries,
					studyIndex:         studyIndex,
					seriesIndex:        seriesIndex,
					seriesHasImages:    len(instanceRows) > 0,
					seriesImagesLoaded: len(instanceRows) > 0 && !seriesImagesCollapsed,
					series:             series,
				})
				if seriesImagesCollapsed {
					continue
				}
				for instanceIndex, instance := range instanceRows {
					rows = append(rows, archiveBrowserRow{
						kind:          archiveRowInstance,
						studyIndex:    studyIndex,
						seriesIndex:   seriesIndex,
						instanceIndex: instanceIndex,
						series:        series,
						instance:      instance,
					})
				}
			}
		}
	}
	return rows
}

func archiveBrowserRowsForState(state *uiState) []archiveBrowserRow {
	if state == nil {
		return nil
	}
	return archiveBrowserRowsWithInlineSeriesInstancesAndCollapsed(state.studies, state.collapsedPatientGroups, state.archiveSeriesByStudy, state.archiveInstancesBySeries, state.collapsedArchiveStudies, state.collapsedArchiveSeries)
}

func archivePatientKey(study archive.Study) string {
	patientID := strings.TrimSpace(study.PatientID)
	patientName := strings.TrimSpace(study.PatientName)
	if patientID != "" {
		return "id:" + strings.ToUpper(patientID)
	}
	if patientName != "" {
		return "name:" + strings.ToUpper(patientName)
	}
	return "missing:" + strings.TrimSpace(study.StudyInstanceUID)
}

func mergeModalities(existing string, next string) string {
	seen := map[string]bool{}
	var values []string
	for _, source := range []string{existing, next} {
		for _, part := range strings.FieldsFunc(source, func(r rune) bool {
			return r == ',' || r == '\\' || r == '/' || r == ';' || r == ' '
		}) {
			part = strings.ToUpper(strings.TrimSpace(part))
			if part == "" || seen[part] {
				continue
			}
			seen[part] = true
			values = append(values, part)
		}
	}
	return strings.Join(values, "\\")
}

func archiveBrowserCell(row archiveBrowserRow, studies []archive.Study, col int) string {
	if row.kind == archiveRowInstance {
		switch col {
		case archiveStudyTableColumnPatient:
			return archiveInlineInstanceLabel(row.instance)
		case archiveStudyTableColumnModality:
			return row.instance.Modality
		case archiveStudyTableColumnDescription:
			return row.instance.SOPClassUID
		case archiveStudyTableColumnInstances:
			return "1"
		case archiveStudyTableColumnSeries:
			return row.instance.InstanceNumber
		case archiveStudyTableColumnStudyUID:
			return row.instance.SOPInstanceUID
		default:
			return ""
		}
	}

	if row.kind == archiveRowSeries {
		switch col {
		case archiveStudyTableColumnPatient:
			return archiveInlineSeriesLabel(row.series)
		case archiveStudyTableColumnModality:
			return row.series.Modality
		case archiveStudyTableColumnDescription:
			return row.series.SeriesDescription
		case archiveStudyTableColumnStudyDate:
			return archiveDateTimeCell(row.series.SeriesDate, row.series.SeriesTime)
		case archiveStudyTableColumnTime:
			return dicomTimeCell(row.series.SeriesTime)
		case archiveStudyTableColumnSeries:
			return row.series.SeriesNumber
		case archiveStudyTableColumnInstances:
			return workstationCountCell(strconv.Itoa(row.series.InstanceCount))
		case archiveStudyTableColumnStudyUID:
			return row.series.SeriesInstanceUID
		default:
			return ""
		}
	}

	if row.kind == archiveRowStudy {
		if row.studyIndex < 0 || row.studyIndex >= len(studies) {
			return ""
		}
		study := studies[row.studyIndex]
		if col == archiveStudyTableColumnPatient {
			label := strings.TrimSpace(study.StudyDescription)
			if label == "" {
				label = strings.TrimSpace(study.StudyDate)
			}
			if label == "" {
				label = strings.TrimSpace(study.StudyInstanceUID)
			}
			return emptyDash(label)
		}
		return studyCell(study, col)
	}

	switch col {
	case archiveStudyTableColumnPatient:
		return emptyDash(row.patientName)
	case archiveStudyTableColumnPatientID:
		return emptyDash(row.patientID)
	case archiveStudyTableColumnDOB:
		return emptyDash(compactDisplayDate(row.patientBirthDate))
	case archiveStudyTableColumnModality:
		return row.modalities
	case archiveStudyTableColumnStudyDate:
		return archiveDateTimeCell(row.studyDate, row.studyTime)
	case archiveStudyTableColumnAdded:
		return archiveTimestampCell(row.importedAt)
	case archiveStudyTableColumnInstitution:
		return emptyDash(row.institutionName)
	case archiveStudyTableColumnSeries:
		return workstationCountCell(strconv.Itoa(row.seriesCount))
	case archiveStudyTableColumnInstances:
		return workstationCountCell(strconv.Itoa(row.instanceCount))
	default:
		return ""
	}
}

func archiveInlineSeriesLabel(series archive.Series) string {
	if description := strings.TrimSpace(series.SeriesDescription); description != "" {
		return description
	}
	if number := strings.TrimSpace(series.SeriesNumber); number != "" {
		return "Series " + number
	}
	if modality := strings.TrimSpace(series.Modality); modality != "" {
		return modality + " series"
	}
	return "Series"
}

func archiveInlineInstanceLabel(instance archive.Instance) string {
	if number := strings.TrimSpace(instance.InstanceNumber); number != "" {
		return "• Image " + number
	}
	if modality := strings.TrimSpace(instance.Modality); modality != "" {
		return "• " + modality + " Image"
	}
	return "• Image"
}

func toggleArchivePatientGroup(state *uiState, row archiveBrowserRow) bool {
	if state == nil || row.kind != archiveRowPatient || strings.TrimSpace(row.groupKey) == "" {
		return false
	}
	if state.collapsedPatientGroups == nil {
		state.collapsedPatientGroups = map[string]bool{}
	}
	state.collapsedPatientGroups[row.groupKey] = !row.collapsed
	state.archiveRows = archiveBrowserRowsForState(state)
	state.selectedStudyRow = -1
	return true
}

func toggleArchiveStudySeries(state *uiState, row archiveBrowserRow) bool {
	if state == nil || row.kind != archiveRowStudy || row.studyIndex < 0 || row.studyIndex >= len(state.studies) {
		return false
	}
	studyUID := strings.TrimSpace(state.studies[row.studyIndex].StudyInstanceUID)
	if studyUID == "" {
		return false
	}
	seriesRows, ok := state.archiveSeriesByStudy[studyUID]
	if !ok || len(seriesRows) == 0 {
		return false
	}
	if state.collapsedArchiveStudies == nil {
		state.collapsedArchiveStudies = map[string]bool{}
	}
	state.collapsedArchiveStudies[studyUID] = !state.collapsedArchiveStudies[studyUID]
	state.archiveRows = archiveBrowserRowsForState(state)
	return true
}

func toggleArchiveSeriesImages(state *uiState, row archiveBrowserRow) bool {
	if state == nil || row.kind != archiveRowSeries {
		return false
	}
	seriesUID := strings.TrimSpace(row.series.SeriesInstanceUID)
	if seriesUID == "" {
		if row.studyIndex < 0 || row.studyIndex >= len(state.studies) {
			return false
		}
		studyUID := strings.TrimSpace(state.studies[row.studyIndex].StudyInstanceUID)
		seriesRows := state.archiveSeriesByStudy[studyUID]
		if row.seriesIndex < 0 || row.seriesIndex >= len(seriesRows) {
			return false
		}
		seriesUID = strings.TrimSpace(seriesRows[row.seriesIndex].SeriesInstanceUID)
	}
	if seriesUID == "" || len(state.archiveInstancesBySeries[seriesUID]) == 0 {
		return false
	}
	if state.collapsedArchiveSeries == nil {
		state.collapsedArchiveSeries = map[string]bool{}
	}
	state.collapsedArchiveSeries[seriesUID] = !state.collapsedArchiveSeries[seriesUID]
	state.selectedInstanceRow = -1
	state.archiveRows = archiveBrowserRowsForState(state)
	return true
}

func selectedStudy(state *uiState) (archive.Study, bool) {
	if state == nil || len(state.studies) == 0 {
		return archive.Study{}, false
	}
	row := state.selectedStudyRow
	if row < 0 || row >= len(state.studies) {
		return archive.Study{}, false
	}
	return state.studies[row], true
}

func recordOpenedArchiveStudy(state *uiState, study archive.Study) {
	if state == nil {
		return
	}
	uid := strings.TrimSpace(study.StudyInstanceUID)
	if uid == "" {
		return
	}
	if state.openedArchiveStudyUIDs == nil {
		state.openedArchiveStudyUIDs = map[string]bool{}
	}
	state.openedArchiveStudyUIDs[uid] = true
	state.appConfig.OpenedArchiveStudyUIDs = prependOpenedArchiveStudyUID(state.appConfig.OpenedArchiveStudyUIDs, uid)
	if state.appConfigPath != "" {
		_ = appconfig.Save(state.appConfigPath, state.appConfig)
	}
	if state.archiveAlbumList != nil {
		state.archiveAlbumList.Refresh()
	}
}

func prependOpenedArchiveStudyUID(values []string, uid string) []string {
	uid = strings.TrimSpace(uid)
	if uid == "" {
		return normalizeOpenedArchiveStudyUIDs(values)
	}
	out := []string{uid}
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" || value == uid {
			continue
		}
		out = append(out, value)
		if len(out) >= maxOpenedArchiveStudyUIDs {
			break
		}
	}
	return out
}

func normalizeOpenedArchiveStudyUIDs(values []string) []string {
	out := make([]string, 0, len(values))
	seen := map[string]bool{}
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" || seen[value] {
			continue
		}
		seen[value] = true
		out = append(out, value)
		if len(out) >= maxOpenedArchiveStudyUIDs {
			break
		}
	}
	return out
}

func openedArchiveStudyUIDMap(values []string) map[string]bool {
	values = normalizeOpenedArchiveStudyUIDs(values)
	if len(values) == 0 {
		return nil
	}
	out := make(map[string]bool, len(values))
	for _, value := range values {
		out[value] = true
	}
	return out
}

func selectedSeries(state *uiState) (archive.Series, bool) {
	if state == nil || len(state.series) == 0 {
		return archive.Series{}, false
	}
	row := state.selectedSeriesRow
	if row < 0 || row >= len(state.series) {
		return archive.Series{}, false
	}
	return state.series[row], true
}

func selectedInstance(state *uiState) (archive.Instance, bool) {
	if state == nil || len(state.instances) == 0 {
		return archive.Instance{}, false
	}
	row := state.selectedInstanceRow
	if row < 0 || row >= len(state.instances) {
		return archive.Instance{}, false
	}
	return state.instances[row], true
}

func selectedNode(state *uiState) (nodes.Node, bool) {
	if state == nil || len(state.nodes) == 0 {
		return nodes.Node{}, false
	}
	row := state.selectedNodeRow
	if row < 0 || row >= len(state.nodes) {
		row = 0
	}
	return state.nodes[row], true
}

func selectedEnabledNode(state *uiState) (nodes.Node, bool) {
	return selectedMatchingNode(state, func(node nodes.Node) bool {
		return node.Enabled()
	})
}

func selectedQueryNode(state *uiState) (nodes.Node, bool) {
	return selectedMatchingNode(state, func(node nodes.Node) bool {
		return node.Enabled() && node.QueryEnabled()
	})
}

func querySourceNodes(state *uiState) []nodes.Node {
	if state == nil {
		return nil
	}
	var out []nodes.Node
	for _, node := range state.nodes {
		if node.Enabled() && node.QueryEnabled() {
			out = append(out, node)
		}
	}
	return out
}

type queryLocalCatalog interface {
	StudyMetadata(context.Context, string) (archive.StudyMetadata, error)
	StudyExists(context.Context, string) (bool, error)
}

func enrichQueryMatchesWithLocalMetadata(ctx context.Context, catalog queryLocalCatalog, matches []query.Match) ([]query.Match, error) {
	out := make([]query.Match, len(matches))
	copy(out, matches)
	if catalog == nil {
		return out, nil
	}
	metadataByStudy := map[string]archive.StudyMetadata{}
	existsByStudy := map[string]bool{}
	for i := range out {
		studyUID := strings.TrimSpace(out[i].StudyInstanceUID)
		if !queryUIDAvailable(studyUID) {
			continue
		}
		exists, ok := existsByStudy[studyUID]
		if !ok {
			var err error
			exists, err = catalog.StudyExists(ctx, studyUID)
			if err != nil {
				return nil, err
			}
			existsByStudy[studyUID] = exists
		}
		if exists {
			out[i].LocalState = queryLocalStatePresent
		}
		metadata, ok := metadataByStudy[studyUID]
		if !ok {
			var err error
			metadata, err = catalog.StudyMetadata(ctx, studyUID)
			if err != nil {
				return nil, err
			}
			metadataByStudy[studyUID] = metadata
		}
		out[i].LocalComments = metadata.Comments
	}
	return out, nil
}

func nodeForQueryMatch(state *uiState, match query.Match) (nodes.Node, bool) {
	if state == nil {
		return nodes.Node{}, false
	}
	if strings.TrimSpace(match.SourceNodeID) != "" {
		for _, node := range state.nodes {
			if node.ID == match.SourceNodeID {
				return node, true
			}
		}
	}
	if strings.TrimSpace(match.SourceNodeName) != "" || strings.TrimSpace(match.SourceHost) != "" || match.SourcePort != 0 {
		for _, node := range state.nodes {
			if strings.EqualFold(node.Name, match.SourceNodeName) && strings.EqualFold(node.Host, match.SourceHost) && node.Port == match.SourcePort {
				return node, true
			}
		}
	}
	return selectedQueryNode(state)
}

func selectedSendNode(state *uiState) (nodes.Node, bool) {
	return selectedMatchingNode(state, func(node nodes.Node) bool {
		return node.Enabled() && node.SendEnabled()
	})
}

func selectedMatchingNode(state *uiState, match func(nodes.Node) bool) (nodes.Node, bool) {
	if state == nil || len(state.nodes) == 0 {
		return nodes.Node{}, false
	}
	row := state.selectedNodeRow
	if row >= 0 && row < len(state.nodes) && match(state.nodes[row]) {
		return state.nodes[row], true
	}
	for _, node := range state.nodes {
		if match(node) {
			return node, true
		}
	}
	return nodes.Node{}, false
}

const (
	elementTableColumnSource = iota
	elementTableColumnTag
	elementTableColumnVR
	elementTableColumnKeyword
	elementTableColumnLength
	elementTableColumnValue
)

const elementSortPreferenceKey = "inspectorElements"

func elementTableHeaders() []string {
	return []string{"Source", "Tag", "VR", "Keyword", "Length", "Value"}
}

func newElementTable(state *uiState) *widget.Table {
	headers := elementTableHeaders()
	var table *widget.Table
	table = widget.NewTable(
		func() (int, int) {
			return len(state.elements) + 1, len(headers)
		},
		func() fyne.CanvasObject {
			return newArchiveTableCell()
		},
		func(id widget.TableCellID, obj fyne.CanvasObject) {
			cell := obj.(*archiveTableCell)
			if id.Row == 0 {
				applyTextTableCell(cell, id.Row, elementHeaderLabel(state, id.Col, headers[id.Col]), true, false)
				return
			}
			elem := state.elements[id.Row-1]
			applyTextTableCell(cell, id.Row, tableCell(elem, id.Col), false, false)
		},
	)
	table.OnSelected = func(id widget.TableCellID) {
		if id.Row == 0 {
			if applyElementSort(state, id.Col) {
				table.Refresh()
			}
		}
	}
	table.SetColumnWidth(0, 72)
	table.SetColumnWidth(1, 105)
	table.SetColumnWidth(2, 52)
	table.SetColumnWidth(3, 210)
	table.SetColumnWidth(4, 82)
	table.SetColumnWidth(5, 520)
	applyCompactTableRows(table)
	return table
}

func applyElementSort(state *uiState, col int) bool {
	if state == nil || !elementColumnSortable(col) {
		return false
	}
	if state.elementSortActive && state.elementSortColumn == col {
		state.elementSortDescending = !state.elementSortDescending
	} else {
		state.elementSortActive = true
		state.elementSortColumn = col
		state.elementSortDescending = false
	}
	sortElementsByColumn(state, col, state.elementSortDescending)
	persistElementSortPreference(state)
	return true
}

func applySavedElementSortPreference(state *uiState) {
	if state == nil {
		return
	}
	pref, ok := state.appConfig.UISortPreferences[elementSortPreferenceKey]
	if !ok || !elementColumnSortable(pref.Column) {
		return
	}
	state.elementSortActive = true
	state.elementSortColumn = pref.Column
	state.elementSortDescending = pref.Descending
	sortElementsByColumn(state, pref.Column, pref.Descending)
}

func sortElementsByColumn(state *uiState, col int, descending bool) {
	if state == nil {
		return
	}
	sort.SliceStable(state.elements, func(i, j int) bool {
		left := elementSortValue(state.elements[i], col)
		right := elementSortValue(state.elements[j], col)
		if left == right {
			left = strings.ToLower(strings.TrimSpace(state.elements[i].Tag))
			right = strings.ToLower(strings.TrimSpace(state.elements[j].Tag))
		}
		if descending {
			return left > right
		}
		return left < right
	})
}

func persistElementSortPreference(state *uiState) {
	if state == nil || state.appConfigPath == "" || !state.elementSortActive || !elementColumnSortable(state.elementSortColumn) {
		return
	}
	if state.appConfig.UISortPreferences == nil {
		state.appConfig.UISortPreferences = map[string]appconfig.SortPreference{}
	}
	state.appConfig.UISortPreferences[elementSortPreferenceKey] = appconfig.SortPreference{
		Column:     state.elementSortColumn,
		Descending: state.elementSortDescending,
	}
	_ = appconfig.Save(state.appConfigPath, state.appConfig)
}

func elementColumnSortable(col int) bool {
	return col >= elementTableColumnSource && col <= elementTableColumnValue
}

func elementSortValue(element dicominspect.ElementSummary, col int) string {
	return strings.ToLower(strings.TrimSpace(tableCell(element, col)))
}

func elementHeaderLabel(state *uiState, col int, label string) string {
	if state == nil {
		return label
	}
	return sortHeaderLabel(label, state.elementSortActive && state.elementSortColumn == col, state.elementSortDescending)
}

var (
	archiveHeaderRowColor                = color.NRGBA{R: 40, G: 40, B: 40, A: 255}
	archivePatientRowColor               = color.NRGBA{R: 48, G: 48, B: 48, A: 255}
	archiveOddRowColor                   = color.NRGBA{R: 28, G: 28, B: 28, A: 255}
	archiveEvenRowColor                  = color.NRGBA{R: 34, G: 34, B: 34, A: 255}
	archiveSeriesRowColor                = color.NRGBA{R: 24, G: 24, B: 24, A: 255}
	archiveInstanceRowColor              = color.NRGBA{R: 18, G: 18, B: 18, A: 255}
	archiveSelectedRowColor              = color.NRGBA{R: 40, G: 92, B: 200, A: 255}
	archiveSummarySelectedStudyRowColor  = color.NRGBA{R: 48, G: 48, B: 48, A: 255}
	nodeDisabledRowColor                 = color.NRGBA{R: 26, G: 26, B: 26, A: 255}
	tableColumnDividerColor              = color.NRGBA{R: 90, G: 90, B: 90, A: 255}
	queryRetrieveActionRowColor          = color.NRGBA{R: 34, G: 58, B: 38, A: 255}
	queryQuickSearchSelectedSegmentColor = color.NRGBA{R: 82, G: 82, B: 82, A: 255}
	archiveMetadataGlyphSlotColor        = color.NRGBA{R: 46, G: 46, B: 46, A: 255}
)

const tableColumnDividerWidth float32 = 1
const compactTableRowHeight float32 = 25
const archiveTableRowHeight float32 = compactTableRowHeight + 1
const queryTableRowHeight float32 = archiveTableRowHeight
const networkTableRowHeight float32 = compactTableRowHeight + 5
const archiveMetadataGlyphSlotWidth float32 = 18

const (
	tableCellVerticalPadding   float32 = 1
	tableCellHorizontalPadding float32 = 4
)

func applyCompactTableRows(table *widget.Table) {
	if table == nil {
		return
	}
	table.SetRowHeight(-1, compactTableRowHeight)
}

func applyArchiveTableRows(table *widget.Table) {
	if table == nil {
		return
	}
	table.SetRowHeight(-1, archiveTableRowHeight)
}

func applyQueryTableRows(table *widget.Table) {
	if table == nil {
		return
	}
	table.SetRowHeight(-1, queryTableRowHeight)
}

const queryResultsViewportMinHeight float32 = queryTableRowHeight * 5

type queryWorkspaceLayout struct{}

func (queryWorkspaceLayout) Layout(objects []fyne.CanvasObject, size fyne.Size) {
	visible := visibleObjects(objects)
	if len(visible) == 0 {
		return
	}
	if len(visible) == 1 {
		visible[0].Move(fyne.NewPos(0, 0))
		visible[0].Resize(size)
		return
	}

	criteria := visible[0]
	results := visible[1]
	resultsMinHeight := fyne.Max(results.MinSize().Height, queryResultsViewportMinHeight)
	// Give the criteria only as much height as its content needs so the results
	// table fills the rest of the window instead of leaving a dead gap. The
	// criteria is wrapped in a vertical scroll whose own MinSize is tiny, so
	// measure the scrolled content directly. When the criteria are taller than
	// the available space (e.g. Advanced Criteria open) they are capped and
	// scroll, keeping the results table at its minimum.
	criteriaHeight := criteria.MinSize().Height
	if scroll, ok := criteria.(*container.Scroll); ok && scroll.Content != nil {
		criteriaHeight = scroll.Content.MinSize().Height
	}
	maxCriteriaHeight := size.Height - resultsMinHeight
	if criteriaHeight > maxCriteriaHeight {
		criteriaHeight = maxCriteriaHeight
	}
	if criteriaHeight > size.Height {
		criteriaHeight = size.Height
	}
	if criteriaHeight < 0 {
		criteriaHeight = 0
	}
	resultsHeight := size.Height - criteriaHeight
	if resultsHeight < 0 {
		resultsHeight = 0
	}

	criteria.Move(fyne.NewPos(0, 0))
	criteria.Resize(fyne.NewSize(size.Width, criteriaHeight))
	results.Move(fyne.NewPos(0, criteriaHeight))
	results.Resize(fyne.NewSize(size.Width, resultsHeight))
}

func (queryWorkspaceLayout) MinSize(objects []fyne.CanvasObject) fyne.Size {
	visible := visibleObjects(objects)
	if len(visible) == 0 {
		return fyne.NewSize(0, 0)
	}
	if len(visible) == 1 {
		return visible[0].MinSize()
	}
	criteriaMin := visible[0].MinSize()
	resultsMin := visible[1].MinSize()
	return fyne.NewSize(
		fyne.Max(criteriaMin.Width, resultsMin.Width),
		criteriaMin.Height+fyne.Max(resultsMin.Height, queryResultsViewportMinHeight),
	)
}

func visibleObjects(objects []fyne.CanvasObject) []fyne.CanvasObject {
	var visible []fyne.CanvasObject
	for _, object := range objects {
		if object != nil && object.Visible() {
			visible = append(visible, object)
		}
	}
	return visible
}

func applyNetworkTableRows(table *widget.Table) {
	if table == nil {
		return
	}
	table.SetRowHeight(-1, networkTableRowHeight)
}

type rightDividerLayout struct{}

type leftDividerLayout struct{}

type archiveMetadataGlyphSlotLayout struct{}

func (leftDividerLayout) Layout(objects []fyne.CanvasObject, size fyne.Size) {
	if len(objects) == 0 {
		return
	}
	objects[0].Move(fyne.NewPos(0, 0))
	objects[0].Resize(fyne.NewSize(tableColumnDividerWidth, size.Height))
}

func (leftDividerLayout) MinSize([]fyne.CanvasObject) fyne.Size {
	return fyne.NewSize(tableColumnDividerWidth, 1)
}

func (rightDividerLayout) Layout(objects []fyne.CanvasObject, size fyne.Size) {
	if len(objects) == 0 {
		return
	}
	x := size.Width - tableColumnDividerWidth
	if x < 0 {
		x = 0
	}
	objects[0].Move(fyne.NewPos(x, 0))
	objects[0].Resize(fyne.NewSize(tableColumnDividerWidth, size.Height))
}

func (rightDividerLayout) MinSize([]fyne.CanvasObject) fyne.Size {
	return fyne.NewSize(tableColumnDividerWidth, 1)
}

func (archiveMetadataGlyphSlotLayout) Layout(objects []fyne.CanvasObject, size fyne.Size) {
	if len(objects) == 0 {
		return
	}
	width := archiveMetadataGlyphSlotWidth
	if width > size.Width {
		width = size.Width
	}
	objects[0].Move(fyne.NewPos(size.Width-width, 0))
	objects[0].Resize(fyne.NewSize(width, size.Height))
}

func (archiveMetadataGlyphSlotLayout) MinSize([]fyne.CanvasObject) fyne.Size {
	return fyne.NewSize(archiveMetadataGlyphSlotWidth, 1)
}

type bottomDividerLayout struct{}

func (bottomDividerLayout) Layout(objects []fyne.CanvasObject, size fyne.Size) {
	if len(objects) == 0 {
		return
	}
	y := size.Height - tableColumnDividerWidth
	if y < 0 {
		y = 0
	}
	objects[0].Move(fyne.NewPos(0, y))
	objects[0].Resize(fyne.NewSize(size.Width, tableColumnDividerWidth))
}

func (bottomDividerLayout) MinSize([]fyne.CanvasObject) fyne.Size {
	return fyne.NewSize(1, tableColumnDividerWidth)
}

func newTableColumnDividerLayer() *fyne.Container {
	return container.New(rightDividerLayout{}, canvas.NewRectangle(tableColumnDividerColor))
}

func newTableLeadingColumnDividerLayer() *fyne.Container {
	return container.New(leftDividerLayout{}, canvas.NewRectangle(tableColumnDividerColor))
}

func newTableRowDividerLayer() *fyne.Container {
	return container.New(bottomDividerLayout{}, canvas.NewRectangle(tableColumnDividerColor))
}

func newCompactTableCellContent(content fyne.CanvasObject) *fyne.Container {
	return container.New(
		layout.NewCustomPaddedLayout(tableCellVerticalPadding, tableCellVerticalPadding, tableCellHorizontalPadding, tableCellHorizontalPadding),
		content,
	)
}

type archiveTableCell struct {
	widget.BaseWidget
	Container         *fyne.Container
	background        *canvas.Rectangle
	statusChip        *canvas.Rectangle
	metadataGlyphSlot *canvas.Rectangle
	label             *widget.Label
	sortLabel         *widget.Label
	statusDot         *canvas.Circle
	statusDotBox      *fyne.Container
	disclosure        *archiveDisclosureArrow
}

const (
	archiveDisclosureArrowHitWidth float32 = 16 // glyph slot width (also the hit target)
	archiveDisclosureIndentUnit    float32 = 12 // leading indent added per tree depth
)

// archiveDisclosureArrow is the tappable tree-disclosure control shown at the
// leading edge of the patient column. Because the Fyne driver dispatches a tap
// to the innermost fyne.Tappable under the cursor, tapping the arrow toggles
// expansion (its onTap) while tapping the row text falls through to the table's
// own selection handling. It carries the per-depth indentation so leaf and
// branch rows line up. It renders the same monochrome chevron resources Fyne's
// own tree uses, which avoids the colour-emoji presentation of the Unicode
// triangle code points.
type archiveDisclosureArrow struct {
	widget.BaseWidget
	glyph  *widget.Label
	indent float32
	onTap  func()
}

func newArchiveDisclosureArrow() *archiveDisclosureArrow {
	label := widget.NewLabel("")
	label.Alignment = fyne.TextAlignCenter
	a := &archiveDisclosureArrow{glyph: label}
	a.ExtendBaseWidget(a)
	return a
}

func (a *archiveDisclosureArrow) Tapped(_ *fyne.PointEvent) {
	if a.onTap != nil {
		a.onTap()
	}
}

func (a *archiveDisclosureArrow) configure(glyph string, indentLevel int, onTap func()) {
	a.glyph.SetText(glyph)
	a.indent = float32(indentLevel) * archiveDisclosureIndentUnit
	a.onTap = onTap
	a.Refresh()
}

func (a *archiveDisclosureArrow) CreateRenderer() fyne.WidgetRenderer {
	return &archiveDisclosureArrowRenderer{arrow: a}
}

type archiveDisclosureArrowRenderer struct {
	arrow *archiveDisclosureArrow
}

func (r *archiveDisclosureArrowRenderer) Layout(size fyne.Size) {
	glyphSize := r.arrow.glyph.MinSize()
	r.arrow.glyph.Resize(glyphSize)
	r.arrow.glyph.Move(fyne.NewPos(r.arrow.indent, (size.Height-glyphSize.Height)/2))
}

func (r *archiveDisclosureArrowRenderer) MinSize() fyne.Size {
	glyphSize := r.arrow.glyph.MinSize()
	return fyne.NewSize(r.arrow.indent+archiveDisclosureArrowHitWidth, glyphSize.Height)
}

func (r *archiveDisclosureArrowRenderer) Refresh() {
	r.arrow.glyph.Refresh()
	canvas.Refresh(r.arrow)
}

func (r *archiveDisclosureArrowRenderer) Objects() []fyne.CanvasObject {
	return []fyne.CanvasObject{r.arrow.glyph}
}

func (r *archiveDisclosureArrowRenderer) Destroy() {}

func archiveRowIndentLevel(row archiveBrowserRow) int {
	switch row.kind {
	case archiveRowStudy:
		return 1
	case archiveRowSeries:
		return 2
	case archiveRowInstance:
		return 3
	default:
		return 0
	}
}

func archiveRowExpandable(row archiveBrowserRow) bool {
	switch row.kind {
	case archiveRowPatient:
		return true
	case archiveRowStudy:
		return row.studyHasSeries
	case archiveRowSeries:
		return row.seriesHasImages || row.series.InstanceCount > 0
	default:
		return false
	}
}

func archiveRowExpanded(row archiveBrowserRow) bool {
	switch row.kind {
	case archiveRowPatient:
		return !row.collapsed
	case archiveRowStudy:
		return row.studySeriesLoaded
	case archiveRowSeries:
		return row.seriesImagesLoaded
	default:
		return false
	}
}

func archiveRowDisclosureGlyph(row archiveBrowserRow) string {
	if !archiveRowExpandable(row) {
		return ""
	}
	if archiveRowExpanded(row) {
		return "▾"
	}
	return "▸"
}

// configureArchiveDisclosure wires the disclosure arrow for a rendered cell.
// Only the patient column carries the arrow; for expandable rows it toggles, and
// for leaf rows it still occupies the indent and selects the row so taps in the
// leading gutter behave like taps on the name.
func configureArchiveDisclosure(cell *archiveTableCell, col int, row archiveBrowserRow, state *uiState) {
	if cell == nil || cell.disclosure == nil {
		return
	}
	if col != archiveStudyTableColumnPatient {
		cell.disclosure.Hide()
		return
	}
	indent := archiveRowIndentLevel(row)
	captured := row
	if archiveRowExpandable(row) {
		cell.disclosure.configure(archiveRowDisclosureGlyph(row), indent, func() {
			if state != nil && state.archiveToggleRow != nil {
				state.archiveToggleRow(captured)
			}
		})
	} else {
		cell.disclosure.configure("", indent, func() {
			if state != nil && state.archiveSelectRow != nil {
				state.archiveSelectRow(captured)
			}
		})
	}
	cell.disclosure.Show()
}

func storeArchiveSeries(state *uiState, studyUID string, series []archive.Series) {
	if state == nil || strings.TrimSpace(studyUID) == "" {
		return
	}
	if state.archiveSeriesByStudy == nil {
		state.archiveSeriesByStudy = map[string][]archive.Series{}
	}
	state.archiveSeriesByStudy[studyUID] = series
}

func storeArchiveInstances(state *uiState, seriesUID string, instances []archive.Instance) {
	if state == nil || strings.TrimSpace(seriesUID) == "" {
		return
	}
	if state.archiveInstancesBySeries == nil {
		state.archiveInstancesBySeries = map[string][]archive.Instance{}
	}
	if len(instances) == 0 {
		delete(state.archiveInstancesBySeries, seriesUID)
		return
	}
	state.archiveInstancesBySeries[seriesUID] = instances
}

func newArchiveTableCell() *archiveTableCell {
	background := canvas.NewRectangle(archiveOddRowColor)
	statusChip := canvas.NewRectangle(color.NRGBA{})
	statusChip.Hide()
	metadataGlyphSlot := canvas.NewRectangle(color.NRGBA{})
	metadataGlyphSlot.Hide()
	label := widget.NewLabel("wide table cell value")
	label.Wrapping = fyne.TextTruncate
	sortLabel := widget.NewLabel("")
	sortLabel.Alignment = fyne.TextAlignTrailing
	sortLabel.TextStyle = fyne.TextStyle{Bold: true}
	sortLabel.Hide()
	statusDot := newSourceStatusDot()
	statusDotBox := container.NewPadded(sourceStatusDotBox(statusDot))
	statusDotBox.Hide()
	disclosure := newArchiveDisclosureArrow()
	disclosure.Hide()
	leftSlot := container.NewHBox(disclosure, statusDotBox)
	labelRow := container.NewBorder(nil, nil, leftSlot, sortLabel, label)
	cell := &archiveTableCell{
		Container:         container.NewStack(background, statusChip, container.New(archiveMetadataGlyphSlotLayout{}, metadataGlyphSlot), newCompactTableCellContent(labelRow), newTableColumnDividerLayer(), newTableRowDividerLayer()),
		background:        background,
		statusChip:        statusChip,
		metadataGlyphSlot: metadataGlyphSlot,
		label:             label,
		sortLabel:         sortLabel,
		statusDot:         statusDot,
		statusDotBox:      statusDotBox,
		disclosure:        disclosure,
	}
	cell.ExtendBaseWidget(cell)
	return cell
}

func (cell *archiveTableCell) CreateRenderer() fyne.WidgetRenderer {
	return widget.NewSimpleRenderer(cell.Container)
}

type queryTableCell struct {
	widget.BaseWidget
	Container      *fyne.Container
	background     *canvas.Rectangle
	label          *widget.Label
	sortLabel      *widget.Label
	retrieveButton *widget.Button
	statusDot      *canvas.Circle
	statusDotBox   *fyne.Container
}

func newQueryTableCell() *queryTableCell {
	background := canvas.NewRectangle(archiveOddRowColor)
	label := widget.NewLabel("wide table cell value")
	label.Wrapping = fyne.TextTruncate
	sortLabel := widget.NewLabel("")
	sortLabel.Alignment = fyne.TextAlignTrailing
	sortLabel.TextStyle = fyne.TextStyle{Bold: true}
	sortLabel.Hide()
	retrieveButton := newQueryRetrieveButton(nil)
	retrieveButton.Hide()
	statusDot := canvas.NewCircle(queryStatusOKColor)
	statusDot.StrokeColor = color.NRGBA{R: 26, G: 26, B: 26, A: 255}
	statusDot.StrokeWidth = 1
	statusDotBox := container.NewPadded(sourceStatusDotBox(statusDot))
	statusDotBox.Hide()
	labelRow := container.NewBorder(nil, nil, statusDotBox, sortLabel, label)
	cell := &queryTableCell{
		Container:      container.NewStack(background, newCompactTableCellContent(labelRow), container.NewCenter(retrieveButton), newTableColumnDividerLayer(), newTableRowDividerLayer()),
		background:     background,
		label:          label,
		sortLabel:      sortLabel,
		retrieveButton: retrieveButton,
		statusDot:      statusDot,
		statusDotBox:   statusDotBox,
	}
	cell.ExtendBaseWidget(cell)
	return cell
}

func (cell *queryTableCell) CreateRenderer() fyne.WidgetRenderer {
	return widget.NewSimpleRenderer(cell.Container)
}

func newQueryRetrieveButton(tapped func()) *widget.Button {
	button := widget.NewButtonWithIcon("", queryRetrieveRowIconResource, tapped)
	button.Importance = widget.LowImportance
	return button
}

func applyArchiveTableCell(cell *archiveTableCell, tableRow int, text string, row archiveBrowserRow, header bool, selected bool) {
	applyArchiveTableCellWithColumn(cell, tableRow, -1, text, row, header, selected)
}

func applyArchiveTableCellWithColumn(cell *archiveTableCell, tableRow int, tableCol int, text string, row archiveBrowserRow, header bool, selected bool) {
	if cell == nil {
		return
	}
	cell.statusDotBox.Hide()
	cell.statusChip.Hide()
	cell.sortLabel.Hide()
	if cell.disclosure != nil {
		cell.disclosure.Hide()
	}
	cell.statusChip.FillColor = color.NRGBA{}
	cell.metadataGlyphSlot.Hide()
	cell.metadataGlyphSlot.FillColor = color.NRGBA{}
	cell.label.SetText(text)
	cell.label.TextStyle = fyne.TextStyle{
		Bold:   header || row.kind == archiveRowPatient || row.kind == archiveRowSeries,
		Italic: row.kind == archiveRowInstance,
	}
	cell.background.FillColor = archiveTableFillColor(tableRow, row, header, selected)
	if !header && (tableCol == archiveStudyTableColumnStatus || tableCol == archiveStudyTableColumnComments) {
		if !selected {
			cell.metadataGlyphSlot.FillColor = archiveMetadataGlyphSlotColor
			cell.metadataGlyphSlot.Show()
			cell.metadataGlyphSlot.Refresh()
		}
		cell.sortLabel.SetText("▾")
		cell.sortLabel.Show()
		cell.sortLabel.Refresh()
	}
	if !header && tableCol == archiveStudyTableColumnStatus {
		if fill, ok := studyStatusDotColor(text); ok {
			cell.statusDot.FillColor = fill
			cell.statusDot.Refresh()
			cell.statusDotBox.Show()
		}
		if fill, ok := studyStatusChipColor(text); ok && !selected {
			cell.statusChip.FillColor = fill
			cell.statusChip.Show()
			cell.statusChip.Refresh()
		}
	}
	cell.background.Refresh()
	cell.label.Refresh()
}

func applyArchiveHeaderTableCell(cell *archiveTableCell, tableCol int, text string, state *uiState) {
	applyArchiveTableCell(cell, 0, text, archiveBrowserRow{}, true, false)
	if cell == nil {
		return
	}
	glyph := archiveHeaderSortGlyph(state, tableCol)
	if glyph == "" {
		return
	}
	cell.sortLabel.SetText(glyph)
	cell.sortLabel.Show()
	cell.sortLabel.Refresh()
}

func applyTextTableCell(cell *archiveTableCell, tableRow int, text string, header bool, selected bool) {
	applyArchiveTableCell(cell, tableRow, text, archiveBrowserRow{kind: archiveRowStudy}, header, selected)
}

func archiveBrowserRowSelected(row archiveBrowserRow, state *uiState) bool {
	if state == nil {
		return false
	}
	switch row.kind {
	case archiveRowPatient:
		return strings.TrimSpace(row.groupKey) != "" && row.groupKey == state.selectedPatientKey
	case archiveRowStudy:
		return row.studyIndex >= 0 && row.studyIndex == state.selectedStudyRow && state.selectedSeriesRow < 0
	case archiveRowSeries:
		return row.studyIndex >= 0 &&
			row.seriesIndex >= 0 &&
			row.studyIndex == state.selectedStudyRow &&
			row.seriesIndex == state.selectedSeriesRow
	case archiveRowInstance:
		return row.studyIndex >= 0 &&
			row.seriesIndex >= 0 &&
			row.instanceIndex >= 0 &&
			row.studyIndex == state.selectedStudyRow &&
			row.seriesIndex == state.selectedSeriesRow &&
			row.instanceIndex == state.selectedInstanceRow
	default:
		return false
	}
}

func archiveTableFillColor(tableRow int, row archiveBrowserRow, header bool, selected bool) color.NRGBA {
	if header {
		return archiveHeaderRowColor
	}
	if selected {
		return archiveSelectedRowColor
	}
	if row.kind == archiveRowPatient {
		return archivePatientRowColor
	}
	if row.kind == archiveRowSeries {
		return archiveSeriesRowColor
	}
	if row.kind == archiveRowInstance {
		return archiveInstanceRowColor
	}
	if tableRow%2 == 0 {
		return archiveEvenRowColor
	}
	return archiveOddRowColor
}

const (
	archiveStudyTableColumnPatient = iota
	archiveStudyTableColumnModality
	archiveStudyTableColumnInstances
	archiveStudyTableColumnSeries
	archiveStudyTableColumnPatientID
	archiveStudyTableColumnDOB
	archiveStudyTableColumnAccession
	archiveStudyTableColumnStudyDate
	archiveStudyTableColumnTime
	archiveStudyTableColumnAdded
	archiveStudyTableColumnInstitution
	archiveStudyTableColumnStatus
	archiveStudyTableColumnComments
	archiveStudyTableColumnDescription
	archiveStudyTableColumnStudyUID
)

func archiveTableHeaders() []string {
	columns := archiveVisibleStudyColumns()
	headers := make([]string, 0, len(columns))
	for _, col := range columns {
		headers = append(headers, archiveStudyTableHeader(col))
	}
	return headers
}

func archiveVisibleStudyColumns() []int {
	return []int{
		archiveStudyTableColumnPatient,
		archiveStudyTableColumnModality,
		archiveStudyTableColumnInstances,
		archiveStudyTableColumnPatientID,
		archiveStudyTableColumnDOB,
		archiveStudyTableColumnStudyDate,
		archiveStudyTableColumnAdded,
		archiveStudyTableColumnInstitution,
		archiveStudyTableColumnStatus,
		archiveStudyTableColumnComments,
	}
}

func archiveVisibleStudyColumn(tableCol int) (int, bool) {
	columns := archiveVisibleStudyColumns()
	if tableCol < 0 || tableCol >= len(columns) {
		return 0, false
	}
	return columns[tableCol], true
}

func archiveStudyMetadataColumn(tableCol int) bool {
	col, ok := archiveVisibleStudyColumn(tableCol)
	if !ok {
		return false
	}
	return col == archiveStudyTableColumnStatus || col == archiveStudyTableColumnComments
}

func archiveStudyTableHeader(col int) string {
	switch col {
	case archiveStudyTableColumnPatient:
		return "Patient name"
	case archiveStudyTableColumnModality:
		return "Modality"
	case archiveStudyTableColumnInstances:
		return "# im"
	case archiveStudyTableColumnSeries:
		return "# ser..."
	case archiveStudyTableColumnPatientID:
		return "Patient ID"
	case archiveStudyTableColumnDOB:
		return "Date of Birth"
	case archiveStudyTableColumnAccession:
		return "Accession..."
	case archiveStudyTableColumnStudyDate:
		return "Date Acquired"
	case archiveStudyTableColumnTime:
		return "Time"
	case archiveStudyTableColumnAdded:
		return "Date Added"
	case archiveStudyTableColumnInstitution:
		return "Institution"
	case archiveStudyTableColumnStatus:
		return "Status"
	case archiveStudyTableColumnComments:
		return "Comments"
	case archiveStudyTableColumnDescription:
		return "Description"
	case archiveStudyTableColumnStudyUID:
		return "Study UID"
	default:
		return ""
	}
}

func newStudyTable(state *uiState) *widget.Table {
	headers := archiveTableHeaders()
	table := widget.NewTable(
		func() (int, int) {
			return len(state.archiveRows) + 1, len(headers)
		},
		func() fyne.CanvasObject {
			return newArchiveTableCell()
		},
		func(id widget.TableCellID, obj fyne.CanvasObject) {
			cell := obj.(*archiveTableCell)
			col, ok := archiveVisibleStudyColumn(id.Col)
			if !ok {
				applyArchiveTableCell(cell, id.Row, "", archiveBrowserRow{}, id.Row == 0, false)
				return
			}
			if id.Row == 0 {
				applyArchiveHeaderTableCell(cell, col, archiveHeaderLabel(state, col, headers[id.Col]), state)
				return
			}
			row := state.archiveRows[id.Row-1]
			selected := archiveBrowserRowSelected(row, state)
			applyArchiveTableCellWithColumn(cell, id.Row, col, archiveBrowserCell(row, state.studies, col), row, false, selected)
			configureArchiveDisclosure(cell, col, row, state)
		},
	)
	state.selectedStudyRow = -1
	widths := archiveStudyColumnWidths()
	for col, width := range widths {
		table.SetColumnWidth(col, width)
	}
	applyArchiveTableRows(table)
	return table
}

const archiveSortPreferenceKey = "archiveStudies"

func archiveStudyColumnWidths() []float32 {
	return []float32{330, 95, 70, 120, 110, 155, 155, 90, 110, 170}
}

func applyArchiveSort(state *uiState, col int) bool {
	if state == nil || !archiveColumnSortable(col) {
		return false
	}
	selectedStudyUID := selectedArchiveStudyUID(state)
	if state.archiveSortActive && state.archiveSortColumn == col {
		state.archiveSortDescending = !state.archiveSortDescending
	} else {
		state.archiveSortActive = true
		state.archiveSortColumn = col
		state.archiveSortDescending = false
	}
	sortArchiveStudiesByColumn(state, col, state.archiveSortDescending)
	persistArchiveSortPreference(state)
	refreshArchiveRowsAfterStudySort(state, selectedStudyUID)
	return true
}

func applySavedArchiveSortPreference(state *uiState) {
	applySavedArchiveSortPreferenceForSelectedUID(state, selectedArchiveStudyUID(state))
}

func applySavedArchiveSortPreferenceForSelectedUID(state *uiState, selectedStudyUID string) {
	if state == nil {
		return
	}
	pref, ok := state.appConfig.UISortPreferences[archiveSortPreferenceKey]
	if !ok || !archiveColumnSortable(pref.Column) {
		pref = appconfig.SortPreference{Column: archiveStudyTableColumnAdded, Descending: true}
	}
	state.archiveSortActive = true
	state.archiveSortColumn = pref.Column
	state.archiveSortDescending = pref.Descending
	sortArchiveStudiesByColumn(state, pref.Column, pref.Descending)
	refreshArchiveRowsAfterStudySort(state, selectedStudyUID)
}

func sortArchiveStudiesByColumn(state *uiState, col int, descending bool) {
	if state == nil {
		return
	}
	sort.SliceStable(state.studies, func(i, j int) bool {
		left := archiveSortValue(state.studies[i], col)
		right := archiveSortValue(state.studies[j], col)
		if left == right {
			left = strings.ToLower(strings.TrimSpace(state.studies[i].StudyInstanceUID))
			right = strings.ToLower(strings.TrimSpace(state.studies[j].StudyInstanceUID))
		}
		if descending {
			return left > right
		}
		return left < right
	})
}

func refreshArchiveRowsAfterStudySort(state *uiState, selectedStudyUID string) {
	if state == nil {
		return
	}
	state.archiveRows = archiveBrowserRowsForState(state)
	state.selectedStudyRow = archiveStudyIndexByUID(state.studies, selectedStudyUID)
	state.selectedSeriesRow = -1
	state.selectedInstanceRow = -1
	state.series = nil
	state.instances = nil
}

func persistArchiveSortPreference(state *uiState) {
	if state == nil || state.appConfigPath == "" || !state.archiveSortActive || !archiveColumnSortable(state.archiveSortColumn) {
		return
	}
	if state.appConfig.UISortPreferences == nil {
		state.appConfig.UISortPreferences = map[string]appconfig.SortPreference{}
	}
	state.appConfig.UISortPreferences[archiveSortPreferenceKey] = appconfig.SortPreference{
		Column:     state.archiveSortColumn,
		Descending: state.archiveSortDescending,
	}
	_ = appconfig.Save(state.appConfigPath, state.appConfig)
}

func selectedArchiveStudyUID(state *uiState) string {
	if state == nil || state.selectedStudyRow < 0 || state.selectedStudyRow >= len(state.studies) {
		return ""
	}
	return strings.TrimSpace(state.studies[state.selectedStudyRow].StudyInstanceUID)
}

func archiveStudyIndexByUID(studies []archive.Study, studyUID string) int {
	studyUID = strings.TrimSpace(studyUID)
	if studyUID == "" {
		return -1
	}
	for index, study := range studies {
		if strings.TrimSpace(study.StudyInstanceUID) == studyUID {
			return index
		}
	}
	return -1
}

func archiveColumnSortable(col int) bool {
	switch col {
	case archiveStudyTableColumnTime, archiveStudyTableColumnDescription, archiveStudyTableColumnStudyUID:
		return false
	default:
		for _, visibleCol := range archiveVisibleStudyColumns() {
			if col == visibleCol {
				return true
			}
		}
		return false
	}
}

func archiveSortValue(study archive.Study, col int) string {
	switch col {
	case archiveStudyTableColumnDOB:
		return strings.TrimSpace(study.PatientBirthDate)
	case archiveStudyTableColumnStudyDate:
		return strings.TrimSpace(study.StudyDate) + strings.TrimSpace(study.StudyTime)
	case archiveStudyTableColumnAdded:
		if study.ImportedAt.IsZero() {
			return ""
		}
		return study.ImportedAt.UTC().Format("20060102150405.000000000")
	case archiveStudyTableColumnSeries:
		return numericSortValue(fmt.Sprintf("%d", study.SeriesCount))
	case archiveStudyTableColumnInstances:
		return numericSortValue(fmt.Sprintf("%d", study.InstanceCount))
	default:
		return strings.ToLower(strings.TrimSpace(studyCell(study, col)))
	}
}

func archiveHeaderLabel(state *uiState, col int, label string) string {
	return label
}

func archiveHeaderSortGlyph(state *uiState, col int) string {
	if state == nil || !state.archiveSortActive || state.archiveSortColumn != col {
		return ""
	}
	if state.archiveSortDescending {
		return "▾"
	}
	return "▴"
}

func newSeriesTable(state *uiState) *widget.Table {
	headers := seriesTableHeaders()
	table := widget.NewTable(
		func() (int, int) {
			return len(state.series) + 1, len(headers)
		},
		func() fyne.CanvasObject {
			return newArchiveTableCell()
		},
		func(id widget.TableCellID, obj fyne.CanvasObject) {
			cell := obj.(*archiveTableCell)
			if id.Row == 0 {
				applyTextTableCell(cell, id.Row, seriesHeaderLabel(state, id.Col, headers[id.Col]), true, false)
				return
			}
			series := state.series[id.Row-1]
			selected := id.Row-1 == state.selectedSeriesRow
			applyTextTableCell(cell, id.Row, seriesCell(series, id.Col), false, selected)
		},
	)
	state.selectedSeriesRow = -1
	table.SetColumnWidth(0, 80)
	table.SetColumnWidth(1, 85)
	table.SetColumnWidth(2, 220)
	table.SetColumnWidth(3, 80)
	table.SetColumnWidth(4, 360)
	applyCompactTableRows(table)
	return table
}

const (
	seriesTableColumnNumber = iota
	seriesTableColumnModality
	seriesTableColumnDescription
	seriesTableColumnInstances
	seriesTableColumnUID
)

const seriesSortPreferenceKey = "archiveSeries"

func seriesTableHeaders() []string {
	return []string{"Series #", "Modality", "Description", "Instances", "Series UID"}
}

func applySeriesSort(state *uiState, col int) bool {
	if state == nil || !seriesColumnSortable(col) {
		return false
	}
	if state.seriesSortActive && state.seriesSortColumn == col {
		state.seriesSortDescending = !state.seriesSortDescending
	} else {
		state.seriesSortActive = true
		state.seriesSortColumn = col
		state.seriesSortDescending = false
	}
	sortSeriesByColumn(state, col, state.seriesSortDescending)
	persistSeriesSortPreference(state)
	resetSeriesDetailSelectionAfterSort(state)
	refreshArchiveRowsAfterSeriesSort(state)
	return true
}

func applySavedSeriesSortPreference(state *uiState) {
	if state == nil {
		return
	}
	pref, ok := state.appConfig.UISortPreferences[seriesSortPreferenceKey]
	if !ok || !seriesColumnSortable(pref.Column) {
		return
	}
	state.seriesSortActive = true
	state.seriesSortColumn = pref.Column
	state.seriesSortDescending = pref.Descending
	sortSeriesByColumn(state, pref.Column, pref.Descending)
}

func sortSeriesByColumn(state *uiState, col int, descending bool) {
	if state == nil {
		return
	}
	sort.SliceStable(state.series, func(i, j int) bool {
		left := seriesSortValue(state.series[i], col)
		right := seriesSortValue(state.series[j], col)
		if left == right {
			left = strings.ToLower(strings.TrimSpace(state.series[i].SeriesInstanceUID))
			right = strings.ToLower(strings.TrimSpace(state.series[j].SeriesInstanceUID))
		}
		if descending {
			return left > right
		}
		return left < right
	})
}

func resetSeriesDetailSelectionAfterSort(state *uiState) {
	if state == nil {
		return
	}
	state.selectedSeriesRow = -1
	state.selectedInstanceRow = -1
	state.instances = nil
}

func persistSeriesSortPreference(state *uiState) {
	if state == nil || state.appConfigPath == "" || !state.seriesSortActive || !seriesColumnSortable(state.seriesSortColumn) {
		return
	}
	if state.appConfig.UISortPreferences == nil {
		state.appConfig.UISortPreferences = map[string]appconfig.SortPreference{}
	}
	state.appConfig.UISortPreferences[seriesSortPreferenceKey] = appconfig.SortPreference{
		Column:     state.seriesSortColumn,
		Descending: state.seriesSortDescending,
	}
	_ = appconfig.Save(state.appConfigPath, state.appConfig)
}

func refreshArchiveRowsAfterSeriesSort(state *uiState) {
	if state == nil || len(state.studies) == 0 {
		return
	}
	if state.archiveSeriesByStudy != nil && state.selectedStudyRow >= 0 && state.selectedStudyRow < len(state.studies) {
		studyUID := strings.TrimSpace(state.studies[state.selectedStudyRow].StudyInstanceUID)
		if studyUID != "" {
			state.archiveSeriesByStudy[studyUID] = state.series
		}
	}
	if state.archiveRows != nil {
		state.archiveRows = archiveBrowserRowsForState(state)
	}
}

func seriesColumnSortable(col int) bool {
	return col >= 0 && col < len(seriesTableHeaders())
}

func seriesSortValue(series archive.Series, col int) string {
	switch col {
	case seriesTableColumnNumber, seriesTableColumnInstances:
		return numericSortValue(seriesCell(series, col))
	default:
		return strings.ToLower(strings.TrimSpace(seriesCell(series, col)))
	}
}

func seriesHeaderLabel(state *uiState, col int, label string) string {
	if state == nil {
		return label
	}
	return sortHeaderLabel(label, state.seriesSortActive && state.seriesSortColumn == col, state.seriesSortDescending)
}

func newInstanceTable(state *uiState) *widget.Table {
	headers := instanceTableHeaders()
	table := widget.NewTable(
		func() (int, int) {
			return len(state.instances) + 1, len(headers)
		},
		func() fyne.CanvasObject {
			return newArchiveTableCell()
		},
		func(id widget.TableCellID, obj fyne.CanvasObject) {
			cell := obj.(*archiveTableCell)
			if id.Row == 0 {
				applyTextTableCell(cell, id.Row, instanceHeaderLabel(state, id.Col, headers[id.Col]), true, false)
				return
			}
			instance := state.instances[id.Row-1]
			selected := id.Row-1 == state.selectedInstanceRow
			applyTextTableCell(cell, id.Row, instanceCell(instance, id.Col), false, selected)
		},
	)
	state.selectedInstanceRow = -1
	table.SetColumnWidth(0, 90)
	table.SetColumnWidth(1, 85)
	table.SetColumnWidth(2, 220)
	table.SetColumnWidth(3, 180)
	table.SetColumnWidth(4, 280)
	table.SetColumnWidth(5, 360)
	applyCompactTableRows(table)
	return table
}

const (
	instanceTableColumnNumber = iota
	instanceTableColumnModality
	instanceTableColumnSOPClass
	instanceTableColumnTransferSyntax
	instanceTableColumnSource
	instanceTableColumnSOPUID
)

const instanceSortPreferenceKey = "archiveInstances"

func instanceTableHeaders() []string {
	return []string{"Instance #", "Modality", "SOP Class", "Transfer Syntax", "Source", "SOP UID"}
}

func applyInstanceSort(state *uiState, col int) bool {
	if state == nil || !instanceColumnSortable(col) {
		return false
	}
	if state.instanceSortActive && state.instanceSortColumn == col {
		state.instanceSortDescending = !state.instanceSortDescending
	} else {
		state.instanceSortActive = true
		state.instanceSortColumn = col
		state.instanceSortDescending = false
	}
	sortInstancesByColumn(state, col, state.instanceSortDescending)
	persistInstanceSortPreference(state)
	resetInstanceSelectionAfterSort(state)
	refreshInlineInstancesAfterSort(state)
	return true
}

func applySavedInstanceSortPreference(state *uiState) {
	if state == nil {
		return
	}
	pref, ok := state.appConfig.UISortPreferences[instanceSortPreferenceKey]
	if !ok || !instanceColumnSortable(pref.Column) {
		return
	}
	state.instanceSortActive = true
	state.instanceSortColumn = pref.Column
	state.instanceSortDescending = pref.Descending
	sortInstancesByColumn(state, pref.Column, pref.Descending)
}

func sortInstancesByColumn(state *uiState, col int, descending bool) {
	if state == nil {
		return
	}
	sort.SliceStable(state.instances, func(i, j int) bool {
		left := instanceSortValue(state.instances[i], col)
		right := instanceSortValue(state.instances[j], col)
		if left == right {
			left = strings.ToLower(strings.TrimSpace(state.instances[i].SOPInstanceUID))
			right = strings.ToLower(strings.TrimSpace(state.instances[j].SOPInstanceUID))
		}
		if descending {
			return left > right
		}
		return left < right
	})
}

func resetInstanceSelectionAfterSort(state *uiState) {
	if state == nil {
		return
	}
	state.selectedInstanceRow = -1
}

func refreshInlineInstancesAfterSort(state *uiState) {
	if state == nil {
		return
	}
	if series, ok := selectedSeries(state); ok {
		seriesUID := strings.TrimSpace(series.SeriesInstanceUID)
		if seriesUID != "" {
			if state.archiveInstancesBySeries == nil {
				state.archiveInstancesBySeries = map[string][]archive.Instance{}
			}
			state.archiveInstancesBySeries[seriesUID] = state.instances
			state.archiveRows = archiveBrowserRowsForState(state)
		}
	}
}

func persistInstanceSortPreference(state *uiState) {
	if state == nil || state.appConfigPath == "" || !state.instanceSortActive || !instanceColumnSortable(state.instanceSortColumn) {
		return
	}
	if state.appConfig.UISortPreferences == nil {
		state.appConfig.UISortPreferences = map[string]appconfig.SortPreference{}
	}
	state.appConfig.UISortPreferences[instanceSortPreferenceKey] = appconfig.SortPreference{
		Column:     state.instanceSortColumn,
		Descending: state.instanceSortDescending,
	}
	_ = appconfig.Save(state.appConfigPath, state.appConfig)
}

func instanceColumnSortable(col int) bool {
	return col >= 0 && col < len(instanceTableHeaders())
}

func instanceSortValue(instance archive.Instance, col int) string {
	switch col {
	case instanceTableColumnNumber:
		return numericSortValue(instanceCell(instance, col))
	default:
		return strings.ToLower(strings.TrimSpace(instanceCell(instance, col)))
	}
}

func instanceHeaderLabel(state *uiState, col int, label string) string {
	if state == nil {
		return label
	}
	return sortHeaderLabel(label, state.instanceSortActive && state.instanceSortColumn == col, state.instanceSortDescending)
}

func numericSortValue(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return ""
	}
	number, err := strconv.Atoi(value)
	if err != nil {
		return "z:" + strings.ToLower(value)
	}
	return fmt.Sprintf("n:%020d", number)
}

func workstationCountCell(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return ""
	}
	number, err := strconv.Atoi(value)
	if err != nil || number < 0 {
		return value
	}
	digits := strconv.Itoa(number)
	if len(digits) <= 3 {
		return digits
	}
	var b strings.Builder
	firstGroup := len(digits) % 3
	if firstGroup == 0 {
		firstGroup = 3
	}
	b.WriteString(digits[:firstGroup])
	for i := firstGroup; i < len(digits); i += 3 {
		b.WriteByte('\'')
		b.WriteString(digits[i : i+3])
	}
	return b.String()
}

// archiveSelectArchiveRow selects a row in the hierarchical study table without
// changing any expand/collapse state. Expansion is driven exclusively by the
// disclosure arrow (see archiveToggleArchiveRow), so clicking a name only
// updates the selection and the right-hand study pane.
func archiveSelectArchiveRow(w fyne.Window, status *widget.Label, tables archiveTables, state *uiState, row archiveBrowserRow, metadataColumn bool) {
	setStatus := func(text string) {
		if status != nil {
			status.SetText(text)
		}
	}
	switch row.kind {
	case archiveRowPatient:
		state.selectedPatientKey = row.groupKey
		state.selectedStudyRow = -1
		state.selectedSeriesRow = -1
		state.selectedInstanceRow = -1
		state.series = nil
		state.instances = nil
		tables.studies.Refresh()
		refreshArchiveChrome(state)
		return
	case archiveRowStudy:
		if row.studyIndex < 0 || row.studyIndex >= len(state.studies) {
			return
		}
		study := state.studies[row.studyIndex]
		state.selectedPatientKey = ""
		state.selectedStudyRow = row.studyIndex
		state.selectedSeriesRow = -1
		state.selectedInstanceRow = -1
		state.series = state.archiveSeriesByStudy[study.StudyInstanceUID]
		state.instances = nil
		recordOpenedArchiveStudy(state, study)
		tables.studies.Refresh()
		refreshArchiveChrome(state)
		if metadataColumn {
			setStatus("Editing status/comments for " + emptyDash(study.StudyInstanceUID))
			if w != nil {
				showStudyMetadataDialog(w, status, tables, state)
			}
			return
		}
		setStatus("Selected study " + emptyDash(study.StudyInstanceUID))
		return
	case archiveRowSeries:
		if row.studyIndex < 0 || row.studyIndex >= len(state.studies) {
			return
		}
		study := state.studies[row.studyIndex]
		seriesRows := state.archiveSeriesByStudy[study.StudyInstanceUID]
		if row.seriesIndex < 0 || row.seriesIndex >= len(seriesRows) {
			return
		}
		series := seriesRows[row.seriesIndex]
		state.selectedPatientKey = ""
		state.selectedStudyRow = row.studyIndex
		state.selectedSeriesRow = row.seriesIndex
		state.selectedInstanceRow = -1
		state.series = seriesRows
		state.instances = state.archiveInstancesBySeries[strings.TrimSpace(series.SeriesInstanceUID)]
		recordOpenedArchiveStudy(state, study)
		tables.studies.Refresh()
		refreshArchiveChrome(state)
		setStatus("Selected series " + emptyDash(series.SeriesInstanceUID))
		return
	case archiveRowInstance:
		if row.studyIndex < 0 || row.studyIndex >= len(state.studies) {
			return
		}
		study := state.studies[row.studyIndex]
		seriesRows := state.archiveSeriesByStudy[study.StudyInstanceUID]
		if row.seriesIndex < 0 || row.seriesIndex >= len(seriesRows) {
			return
		}
		series := seriesRows[row.seriesIndex]
		instanceRows := state.archiveInstancesBySeries[strings.TrimSpace(series.SeriesInstanceUID)]
		if row.instanceIndex < 0 || row.instanceIndex >= len(instanceRows) {
			return
		}
		state.selectedPatientKey = ""
		state.selectedStudyRow = row.studyIndex
		state.selectedSeriesRow = row.seriesIndex
		state.selectedInstanceRow = row.instanceIndex
		state.series = seriesRows
		state.instances = instanceRows
		recordOpenedArchiveStudy(state, study)
		tables.studies.Refresh()
		refreshArchiveChrome(state)
		setStatus("Selected image " + emptyDash(instanceRows[row.instanceIndex].SOPInstanceUID))
		return
	}
}

// archiveToggleArchiveRow expands or collapses the row's children, lazily
// loading series or instances the first time a branch is opened. It never
// changes the current selection, so the disclosure arrow and the row name act
// independently.
func archiveToggleArchiveRow(w fyne.Window, status *widget.Label, tables archiveTables, state *uiState, row archiveBrowserRow) {
	setStatus := func(text string) {
		if status != nil {
			status.SetText(text)
		}
	}
	switch row.kind {
	case archiveRowPatient:
		if toggleArchivePatientGroup(state, row) {
			tables.studies.Refresh()
			refreshArchiveChrome(state)
		}
		return
	case archiveRowStudy:
		if row.studyIndex < 0 || row.studyIndex >= len(state.studies) {
			return
		}
		studyUID := strings.TrimSpace(state.studies[row.studyIndex].StudyInstanceUID)
		if toggleArchiveStudySeries(state, row) {
			tables.studies.Refresh()
			refreshArchiveChrome(state)
			if state.collapsedArchiveStudies[studyUID] {
				setStatus("Collapsed series for " + studyUID)
			} else {
				setStatus("Expanded series for " + studyUID)
			}
			return
		}
		setStatus("Loading series for " + studyUID)
		filters := state.seriesFilters
		go func(studyUID string, filters archive.SeriesFilters) {
			series, err := state.catalog.SeriesForStudyWithFilters(context.Background(), studyUID, filters)
			fyne.Do(func() {
				if err != nil {
					setStatus("Load series failed")
					if w != nil {
						dialog.ShowError(err, w)
					}
					return
				}
				storeArchiveSeries(state, studyUID, series)
				if state.collapsedArchiveStudies == nil {
					state.collapsedArchiveStudies = map[string]bool{}
				}
				state.collapsedArchiveStudies[studyUID] = false
				state.archiveRows = archiveBrowserRowsForState(state)
				tables.studies.Refresh()
				refreshArchiveChrome(state)
				setStatus(fmt.Sprintf("%d series for study %s", len(series), studyUID))
			})
		}(studyUID, filters)
		return
	case archiveRowSeries:
		seriesUID := strings.TrimSpace(row.series.SeriesInstanceUID)
		if seriesUID == "" && row.studyIndex >= 0 && row.studyIndex < len(state.studies) {
			seriesRows := state.archiveSeriesByStudy[state.studies[row.studyIndex].StudyInstanceUID]
			if row.seriesIndex >= 0 && row.seriesIndex < len(seriesRows) {
				seriesUID = strings.TrimSpace(seriesRows[row.seriesIndex].SeriesInstanceUID)
			}
		}
		if toggleArchiveSeriesImages(state, row) {
			tables.studies.Refresh()
			refreshArchiveChrome(state)
			if state.collapsedArchiveSeries[seriesUID] {
				setStatus("Collapsed images for " + seriesUID)
			} else {
				setStatus("Expanded images for " + seriesUID)
			}
			return
		}
		if seriesUID == "" {
			return
		}
		setStatus("Loading images for " + seriesUID)
		go func(seriesUID string) {
			instances, err := state.catalog.InstancesForSeries(context.Background(), seriesUID)
			fyne.Do(func() {
				if err != nil {
					setStatus("Load images failed")
					if w != nil {
						dialog.ShowError(err, w)
					}
					return
				}
				storeArchiveInstances(state, seriesUID, instances)
				if state.collapsedArchiveSeries == nil {
					state.collapsedArchiveSeries = map[string]bool{}
				}
				state.collapsedArchiveSeries[seriesUID] = false
				state.archiveRows = archiveBrowserRowsForState(state)
				tables.studies.Refresh()
				refreshArchiveChrome(state)
				setStatus(fmt.Sprintf("%d images for series %s", len(instances), seriesUID))
			})
		}(seriesUID)
		return
	}
}

func wireArchiveTables(w fyne.Window, status *widget.Label, tables archiveTables, state *uiState) {
	state.archiveSelectRow = func(row archiveBrowserRow) {
		archiveSelectArchiveRow(w, status, tables, state, row, false)
	}
	state.archiveToggleRow = func(row archiveBrowserRow) {
		archiveToggleArchiveRow(w, status, tables, state, row)
	}
	tables.studies.OnSelected = func(id widget.TableCellID) {
		if id.Row <= 0 {
			col, ok := archiveVisibleStudyColumn(id.Col)
			if ok && applyArchiveSort(state, col) {
				tables.studies.Refresh()
				tables.series.Refresh()
				tables.instances.Refresh()
				refreshArchiveChrome(state)
				status.SetText("Sorted Archive by " + archiveTableHeaders()[id.Col])
				return
			}
			state.selectedPatientKey = ""
			state.selectedStudyRow = -1
			clearArchiveDetails(state, tables)
			refreshArchiveChrome(state)
			tables.studies.Refresh()
			return
		}
		rowIndex := id.Row - 1
		if rowIndex < 0 || rowIndex >= len(state.archiveRows) {
			return
		}
		archiveSelectArchiveRow(w, status, tables, state, state.archiveRows[rowIndex], archiveStudyMetadataColumn(id.Col))
	}

	tables.series.OnSelected = func(id widget.TableCellID) {
		if id.Row <= 0 {
			if applySeriesSort(state, id.Col) {
				tables.studies.Refresh()
				tables.series.Refresh()
				tables.instances.Refresh()
				refreshArchiveChrome(state)
				status.SetText("Sorted Series by " + seriesTableHeaders()[id.Col])
				return
			}
			state.selectedSeriesRow = -1
			setInstances(state, tables, nil)
			refreshArchiveChrome(state)
			tables.series.Refresh()
			return
		}
		row := id.Row - 1
		if row < 0 || row >= len(state.series) {
			return
		}
		state.selectedSeriesRow = row
		series := state.series[row]
		tables.series.Refresh()
		setInstances(state, tables, nil)
		refreshArchiveChrome(state)
		status.SetText("Loading instances for " + series.SeriesInstanceUID)
		go func(selectedRow int, seriesUID string) {
			instances, err := state.catalog.InstancesForSeries(context.Background(), seriesUID)
			fyne.Do(func() {
				if state.selectedSeriesRow != selectedRow ||
					selectedRow < 0 ||
					selectedRow >= len(state.series) ||
					state.series[selectedRow].SeriesInstanceUID != seriesUID {
					return
				}
				if err != nil {
					status.SetText("Load instances failed")
					dialog.ShowError(err, w)
					return
				}
				setInstances(state, tables, instances)
				status.SetText(fmt.Sprintf("%d instances for series %s", len(instances), seriesUID))
			})
		}(row, series.SeriesInstanceUID)
	}

	tables.instances.OnSelected = func(id widget.TableCellID) {
		if id.Row <= 0 {
			if applyInstanceSort(state, id.Col) {
				tables.instances.Refresh()
				refreshArchiveChrome(state)
				status.SetText("Sorted Instances by " + instanceTableHeaders()[id.Col])
				return
			}
			state.selectedInstanceRow = -1
			refreshArchiveChrome(state)
			tables.instances.Refresh()
			return
		}
		row := id.Row - 1
		if row >= 0 && row < len(state.instances) {
			state.selectedInstanceRow = row
			refreshArchiveChrome(state)
			tables.instances.Refresh()
		}
	}
}

func newAutoQueryTab(w fyne.Window, status *widget.Label, tables archiveTables, nodeTable *widget.Table, state *uiState) fyne.CanvasObject {
	savedCriteria := autoQueryCriteriaForState(state)
	profileNames := autoQueryProfileNames(state)
	profileSelect := widget.NewSelect(profileNames, nil)
	profileSelect.SetSelected(selectedAutoQueryProfileName(state))
	titleLabel := workbenchSectionTitle(autoQueryWindowTitle(profileSelect.Selected))
	previousButton := autoQueryEnabledIconButton(theme.NavigateBackIcon(), func() {
		selectRelativeAutoQueryProfile(profileSelect, -1)
	})
	nextButton := autoQueryEnabledIconButton(theme.NavigateNextIcon(), func() {
		selectRelativeAutoQueryProfile(profileSelect, 1)
	})
	if len(profileNames) <= 1 {
		previousButton.Disable()
		nextButton.Disable()
	}
	var refreshProfileOptions func(string)
	var syncAutoQueryLockedControls func()
	renameButton := widget.NewButtonWithIcon("", theme.DocumentCreateIcon(), func() {
		if state == nil {
			return
		}
		if w == nil {
			newName := nextAutoQueryProfileName(state.autoQueryProfiles)
			if applyAutoQueryProfileRename(nil, status, state, newName) && refreshProfileOptions != nil {
				refreshProfileOptions(selectedAutoQueryProfileName(state))
			}
			return
		}
		entry := widget.NewEntry()
		entry.SetText(selectedAutoQueryProfileName(state))
		form := dialog.NewForm("Rename Auto Q/R Profile", "Rename", "Cancel", []*widget.FormItem{
			widget.NewFormItem("Name", entry),
		}, func(ok bool) {
			if !ok {
				return
			}
			if applyAutoQueryProfileRename(w, status, state, entry.Text) && refreshProfileOptions != nil {
				refreshProfileOptions(selectedAutoQueryProfileName(state))
			}
		}, w)
		form.Resize(fyne.NewSize(420, 0))
		form.Show()
	})
	renameButton.Importance = widget.LowImportance
	addButton := autoQueryEnabledIconButton(theme.ContentAddIcon(), func() {
		if state == nil {
			return
		}
		profile := addAutoQueryProfile(state)
		if refreshProfileOptions != nil {
			refreshProfileOptions(profile.Name)
		}
		if err := saveAutoQueryProfileList(state); err != nil {
			if status != nil {
				status.SetText("Auto Q/R profile save failed")
			}
			if w != nil {
				dialog.ShowError(err, w)
			}
			return
		}
		if status != nil {
			status.SetText("Auto Q/R profile added")
		}
	})
	removeButton := autoQueryEnabledIconButton(theme.ContentRemoveIcon(), func() {
		if state == nil {
			return
		}
		if !removeSelectedAutoQueryProfile(state) {
			if status != nil {
				status.SetText("Default Auto Q/R profile cannot be removed")
			}
			return
		}
		selected := selectedAutoQueryProfileName(state)
		if refreshProfileOptions != nil {
			refreshProfileOptions(selected)
		}
		if err := saveAutoQueryProfileList(state); err != nil {
			if status != nil {
				status.SetText("Auto Q/R profile save failed")
			}
			if w != nil {
				dialog.ShowError(err, w)
			}
			return
		}
		if status != nil {
			status.SetText("Auto Q/R profile removed")
		}
	})
	lockButton := autoQueryEnabledIconButton(autoQueryProfileLockIcon(), func() {
		if state == nil {
			return
		}
		previous := autoQueryProfileLocked(state)
		setAutoQueryProfileLocked(state, !previous)
		if err := saveAutoQuerySelectedProfile(state); err != nil {
			setAutoQueryProfileLocked(state, previous)
			if status != nil {
				status.SetText("Auto Q/R profile lock save failed")
			}
			if w != nil {
				dialog.ShowError(err, w)
			}
			return
		}
		if status != nil {
			if autoQueryProfileLocked(state) {
				status.SetText("Auto Q/R profile locked")
			} else {
				status.SetText("Auto Q/R profile unlocked")
			}
		}
		if syncAutoQueryLockedControls != nil {
			syncAutoQueryLockedControls()
		}
	})
	updatingProfile := false

	quickSearchField := widget.NewSelect(queryQuickSearchOptions, func(field string) {
		if state != nil {
			state.autoQuerySearchField = field
		}
	})
	quickSearchField.SetSelected(savedCriteria.SearchField)
	quickSearch := widget.NewEntry()
	quickSearch.SetText(savedCriteria.SearchText)
	quickSearch.OnChanged = func(search string) {
		if state != nil {
			state.autoQuerySearchText = strings.TrimSpace(search)
		}
	}
	quickSearchField.OnChanged = func(field string) {
		configureQueryQuickSearchPlaceholder(quickSearch, field)
		if state != nil {
			state.autoQuerySearchField = field
		}
	}
	autoQuerySearchStrip := newQueryQuickSearchFieldStrip(quickSearchField, quickSearch)

	datePreset := newQueryDatePresetRadioGrid(func(preset string) {
		if state != nil {
			state.autoQueryDatePreset = preset
		}
	})
	datePreset.SetSelected(savedCriteria.DatePreset)
	onDate := widget.NewEntry()
	onDateValue := strings.TrimSpace(savedCriteria.OnDate)
	if onDateValue == "" {
		onDateValue = time.Now().Format("20060102")
	}
	onDate.SetText(onDateValue)
	onDate.OnChanged = func(value string) {
		if state != nil {
			state.autoQueryOnDate = strings.TrimSpace(value)
		}
	}
	lastHours := widget.NewEntry()
	lastHoursValue := strings.TrimSpace(savedCriteria.LastHours)
	if lastHoursValue == "" {
		lastHoursValue = "1"
	}
	lastHours.SetText(lastHoursValue)
	lastHours.OnChanged = func(value string) {
		if state != nil {
			state.autoQueryLastHours = strings.TrimSpace(value)
		}
	}
	applyAutoQueryManualDateInput := func() {
		if state != nil {
			state.autoQueryOnDate = strings.TrimSpace(onDate.Text)
			state.autoQueryLastHours = strings.TrimSpace(lastHours.Text)
		}
	}
	modalityChecks := newQueryModalityChecks()
	for _, modality := range savedCriteria.Modalities {
		if check := modalityChecks[modality]; check != nil {
			check.SetChecked(true)
		}
	}
	for _, check := range modalityChecks {
		check.OnChanged = func(_ bool) {
			if state != nil {
				state.autoQueryModalities = selectedQueryModalities(modalityChecks)
			}
		}
	}

	sourceList := newAutoQuerySourceList(w, status, state)
	moveAutoSource := func(delta int) {
		if autoQueryProfileLocked(state) {
			if status != nil {
				status.SetText("Auto Q/R profile is locked")
			}
			return
		}
		if !moveAutoQuerySource(state, delta) {
			return
		}
		if err := saveAutoQuerySelectedProfile(state); err != nil {
			if status != nil {
				status.SetText("Auto Q/R source order save failed")
			}
			if w != nil {
				dialog.ShowError(err, w)
			}
			return
		}
		if status != nil {
			status.SetText("Updated Auto Q/R source priority")
		}
		sourceList.Refresh()
	}
	sourceMoveUp := widget.NewButtonWithIcon("", theme.MoveUpIcon(), func() {
		moveAutoSource(-1)
	})
	sourceMoveUp.Importance = widget.LowImportance
	sourceMoveDown := widget.NewButtonWithIcon("", theme.MoveDownIcon(), func() {
		moveAutoSource(1)
	})
	sourceMoveDown.Importance = widget.LowImportance
	sourceHeader := newDicomNodesHeader(container.NewHBox(sourceMoveUp, sourceMoveDown))
	sourcePanel := newDicomNodesSourcePanel(sourceHeader, container.NewBorder(newQuerySourceColumnHeader(), nil, nil, nil, sourceList))

	destinationSelect := newQueryMoveDestinationEntry(state)
	refreshCadence := widget.NewSelect(autoQueryRefreshModeOptions, nil)
	refreshCadence.SetSelected(savedCriteria.RefreshMode)
	if state != nil {
		state.autoQueryRefreshMode = savedCriteria.RefreshMode
	}
	countdown := widget.NewLabel(autoQueryCountdownText(savedCriteria.RefreshMode, time.Time{}, autoQueryRefreshAvailable(state), time.Now()))
	countdown.Wrapping = fyne.TextTruncate
	if state != nil {
		state.autoQueryCountdownLabel = countdown
		refreshAutoQueryCountdown(state, savedCriteria.RefreshMode, time.Now())
	}
	autoRetrieve := newAutoQueryAutoRetrieveCheck(state)
	settingsButton := widget.NewButton(autoQuerySettingsButtonText, func() {
		showAutoQuerySettingsDialog(w, status, state)
	})
	settingsButton.Importance = widget.LowImportance

	queryTable := newAutoQueryTable(state, func() {
		retrieveSelectedQuery(w, status, tables, state)
	})
	refreshCadence.OnChanged = func(mode string) {
		if state != nil {
			state.autoQueryRefreshMode = mode
		}
		if updatingProfile {
			refreshAutoQueryCountdown(state, mode, time.Now())
			return
		}
		scheduleAutoQueryRefresh(w, status, tables, queryTable, state, mode)
		if state == nil {
			countdown.SetText(autoQueryCountdownText(mode, time.Time{}, false, time.Now()))
		}
	}
	queryButton := newQueryPrimaryActionButton(queryActionLabelQuery, func() {
		profileCriteria := autoQueryCriteriaFromControls(quickSearchField.Selected, quickSearch.Text, datePreset.Selected(), onDate.Text, lastHours.Text, modalityChecks, refreshCadence.Selected)
		criteria, ok := autoQueryStudyCriteriaWithDateInputs(quickSearchField.Selected, quickSearch.Text, datePreset.Selected(), onDate.Text, lastHours.Text, modalityChecks, time.Now())
		if !ok {
			if status != nil {
				status.SetText("Auto Q/R query failed")
			}
			if w != nil {
				dialog.ShowError(fmt.Errorf("unsupported Auto Q/R search field %q", quickSearchField.Selected), w)
			}
			return
		}
		if !saveAutoQueryProfileCriteria(w, status, state, profileCriteria) {
			return
		}
		rememberAutoQueryStudy(state, criteria)
		runStudyQueryWithSources(w, status, queryTable, state, criteria, autoQuerySourceNodes(state), autoQueryMatchesHandler(w, status, tables, state))
		scheduleAutoQueryRefresh(w, status, tables, queryTable, state, refreshCadence.Selected)
	})
	submitAutoStudyQuery := func() {
		if queryButton != nil && queryButton.OnTapped != nil {
			queryButton.OnTapped()
		}
	}
	showAutoQuerySearchFieldMenu := func(anchor fyne.CanvasObject) {
		if autoQueryProfileLocked(state) {
			if status != nil {
				status.SetText("Auto Q/R profile is locked")
			}
			return
		}
		showQueryQuickSearchFieldMenu(anchor, quickSearchField.Selected, func(field string) {
			quickSearchField.SetSelected(field)
		})
	}
	autoQuerySearchBar, autoQuerySearchFieldMenu := newQuerySearchBarWithFieldMenuButton(quickSearch, submitAutoStudyQuery, showAutoQuerySearchFieldMenu)
	patientButton := newQueryPrimaryActionButton(queryActionLabelPatient, func() {
		profileCriteria := autoQueryCriteriaFromControls(quickSearchField.Selected, quickSearch.Text, datePreset.Selected(), onDate.Text, lastHours.Text, modalityChecks, refreshCadence.Selected)
		criteria, ok := autoQueryPatientCriteria(quickSearchField.Selected, quickSearch.Text)
		if !ok {
			if status != nil {
				status.SetText("Auto Q/R patient query failed")
			}
			if w != nil {
				dialog.ShowError(fmt.Errorf("unsupported Auto Q/R Patient search field %q", quickSearchField.Selected), w)
			}
			return
		}
		if !saveAutoQueryProfileCriteria(w, status, state, profileCriteria) {
			return
		}
		rememberAutoQueryPatient(state, criteria)
		runPatientQueryWithSources(w, status, queryTable, state, criteria, autoQuerySourceNodes(state), autoQueryMatchesHandler(w, status, tables, state))
		scheduleAutoQueryRefresh(w, status, tables, queryTable, state, refreshCadence.Selected)
	})
	retrieveButton := disabledAutoQueryAction(queryActionLabelRetrieve)
	verifyButton := newQueryPrimaryActionButton(queryActionLabelVerify, func() {
		verifySelectedNode(w, status, nodeTable, state)
	})
	refreshButton := newQueryRefreshButton(func() {
		refreshAutoQuery(w, status, tables, queryTable, state)
		scheduleAutoQueryRefresh(w, status, tables, queryTable, state, refreshCadence.Selected)
	})
	profileSelect.OnChanged = func(name string) {
		if strings.TrimSpace(name) == "" {
			return
		}
		updatingProfile = true
		defer func() {
			updatingProfile = false
		}()
		if !selectAutoQueryProfile(state, name) {
			return
		}
		criteria := autoQueryCriteriaForState(state)
		quickSearchField.SetSelected(criteria.SearchField)
		configureQueryQuickSearchPlaceholder(quickSearch, criteria.SearchField)
		quickSearch.SetText(criteria.SearchText)
		datePreset.SetSelected(criteria.DatePreset)
		profileOnDate := strings.TrimSpace(criteria.OnDate)
		if profileOnDate == "" {
			profileOnDate = time.Now().Format("20060102")
		}
		onDate.SetText(profileOnDate)
		profileLastHours := strings.TrimSpace(criteria.LastHours)
		if profileLastHours == "" {
			profileLastHours = "1"
		}
		lastHours.SetText(profileLastHours)
		for _, check := range modalityChecks {
			check.SetChecked(false)
		}
		for _, modality := range criteria.Modalities {
			if check := modalityChecks[modality]; check != nil {
				check.SetChecked(true)
			}
		}
		sourceList.Refresh()
		refreshCadence.SetSelected(criteria.RefreshMode)
		refreshAutoQueryCountdown(state, criteria.RefreshMode, time.Now())
		syncAutoQueryProfileButtons(previousButton, nextButton, removeButton, profileSelect.Options, name)
		titleLabel.SetText(autoQueryWindowTitle(name))
		if syncAutoQueryLockedControls != nil {
			syncAutoQueryLockedControls()
		}
	}
	refreshProfileOptions = func(selected string) {
		profileSelect.Options = autoQueryProfileNames(state)
		profileSelect.Refresh()
		syncAutoQueryProfileButtons(previousButton, nextButton, removeButton, profileSelect.Options, selected)
		if stringInList(selected, profileSelect.Options) && profileSelect.Selected != selected {
			profileSelect.SetSelected(selected)
		}
	}
	refreshProfileOptions(selectedAutoQueryProfileName(state))
	syncAutoQueryLockedControls = func() {
		locked := autoQueryProfileLocked(state)
		setDisableableControl(quickSearchField, locked)
		setDisableableControl(quickSearch, locked)
		setDisableableControl(autoQuerySearchFieldMenu, locked)
		datePreset.SetDisabled(locked)
		setDisableableControl(onDate, locked)
		setDisableableControl(lastHours, locked)
		for _, check := range modalityChecks {
			setDisableableControl(check, locked)
		}
		setDisableableControl(refreshCadence, locked)
		setDisableableControl(autoRetrieve, locked)
		setDisableableControl(settingsButton, locked)
		setDisableableControl(sourceMoveUp, locked)
		setDisableableControl(sourceMoveDown, locked)
		if locked || strings.EqualFold(selectedAutoQueryProfileName(state), autoquery.DefaultProfileName) {
			renameButton.Disable()
		} else {
			renameButton.Enable()
		}
		if locked {
			removeButton.Disable()
		} else {
			syncAutoQueryProfileButtons(previousButton, nextButton, removeButton, profileSelect.Options, profileSelect.Selected)
		}
		sourceList.Refresh()
	}
	syncAutoQueryLockedControls()

	resultSummary := widget.NewLabel(autoQueryResultSummaryText(state))
	resultSummary.Wrapping = fyne.TextTruncate
	resultSummary.Alignment = fyne.TextAlignTrailing
	state.autoQueryResultSummaryLabel = resultSummary

	profileBar := container.NewBorder(
		nil,
		nil,
		container.NewHBox(autoQueryProfileIconSlot(previousButton), autoQueryProfileIconSlot(nextButton)),
		container.NewHBox(
			autoQueryProfileIconSlot(addButton),
			autoQueryProfileIconSlot(renameButton),
			autoQueryProfileIconSlot(removeButton),
			autoQueryProfileIconSlot(lockButton),
		),
		autoQueryProfileSelectSlot(profileSelect),
	)
	titleBar := newAutoQueryTitleBar(titleLabel, profileBar)
	searchBar := workbenchStrip(autoQuerySearchBar)
	filters := container.NewHBox(
		sourcePanel,
		workbenchPanelSlot("Date", container.NewVBox(datePreset.CanvasObject(), newQueryManualDateInputs(onDate, lastHours, applyAutoQueryManualDateInput)), queryDateFilterPanelMinWidth),
		workbenchPanelSlot("Modalities", queryModalityGrid(modalityChecks), queryModalityFilterPanelMinWidth),
	)
	refreshCluster := workbenchStrip(container.NewVBox(
		container.NewHBox(queryRefreshCadenceSlot(refreshCadence), autoQueryRefreshCountdownSlot(countdown), autoQueryRefreshButtonSlot(refreshButton)),
		container.NewHBox(queryAutoRetrieveSlot(autoRetrieve), autoQueryRetrieveSettingsSlot(settingsButton)),
	))
	actions := container.NewBorder(
		nil,
		nil,
		newQueryPrimaryActionStrip(queryRetrieveDestinationSlot(labeledControl("Retrieve to:", destinationSelect)), queryButton, patientButton, retrieveButton, verifyButton),
		refreshCluster,
		nil,
	)
	top := container.NewVBox(titleBar, autoQuerySearchStrip, searchBar, filters, actions)
	return container.NewBorder(nil, newAutoQueryFooter(resultSummary), nil, nil, newQueryWorkspace(top, container.NewStack(queryTable)))
}

func autoQueryWindowTitle(profile string) string {
	profile = strings.TrimSpace(profile)
	if profile == "" {
		profile = autoquery.DefaultProfileName
	}
	return "DICOM Auto Query/Retrieve : " + profile
}

func newDicomNodesHeader(actions fyne.CanvasObject) fyne.CanvasObject {
	instruction := widget.NewLabelWithStyle(dicomNodesInstruction, fyne.TextAlignTrailing, fyne.TextStyle{})
	instruction.Wrapping = fyne.TextTruncate
	right := container.NewHBox(instruction)
	if actions != nil {
		right.Add(actions)
	}
	return container.NewStack(
		canvas.NewRectangle(archiveHeaderRowColor),
		newCompactTableCellContent(container.NewBorder(nil, nil, workbenchSectionTitle(dicomNodesTitle), right)),
		newTableColumnDividerLayer(),
		newTableRowDividerLayer(),
	)
}

func autoQueryIconButton(icon fyne.Resource) *widget.Button {
	button := widget.NewButtonWithIcon("", icon, nil)
	button.Importance = widget.LowImportance
	button.Disable()
	return button
}

func autoQueryEnabledIconButton(icon fyne.Resource, tapped func()) *widget.Button {
	button := widget.NewButtonWithIcon("", icon, tapped)
	button.Importance = widget.LowImportance
	return button
}

func autoQueryProfileIconSlot(button *widget.Button) fyne.CanvasObject {
	if button == nil {
		return container.NewGridWrap(fyne.NewSize(autoQueryProfileIconSlotSize, autoQueryProfileIconSlotSize), canvas.NewRectangle(color.Transparent))
	}
	return container.NewGridWrap(fyne.NewSize(autoQueryProfileIconSlotSize, autoQueryProfileIconSlotSize), button)
}

func autoQueryProfileSelectSlot(selectWidget *widget.Select) fyne.CanvasObject {
	if selectWidget == nil {
		return container.NewGridWrap(fyne.NewSize(autoQueryProfileSelectSlotWidth, autoQueryProfileIconSlotSize), canvas.NewRectangle(color.Transparent))
	}
	height := selectWidget.MinSize().Height
	if height < autoQueryProfileIconSlotSize {
		height = autoQueryProfileIconSlotSize
	}
	return container.NewGridWrap(fyne.NewSize(autoQueryProfileSelectSlotWidth, height), selectWidget)
}

func selectRelativeAutoQueryProfile(selectWidget *widget.Select, delta int) {
	if selectWidget == nil || len(selectWidget.Options) == 0 {
		return
	}
	index := 0
	for i, option := range selectWidget.Options {
		if option == selectWidget.Selected {
			index = i
			break
		}
	}
	next := (index + delta + len(selectWidget.Options)) % len(selectWidget.Options)
	selectWidget.SetSelected(selectWidget.Options[next])
}

func syncAutoQueryProfileButtons(previous *widget.Button, next *widget.Button, remove *widget.Button, profiles []string, selected string) {
	if len(profiles) <= 1 {
		if previous != nil {
			previous.Disable()
		}
		if next != nil {
			next.Disable()
		}
	} else {
		if previous != nil {
			previous.Enable()
		}
		if next != nil {
			next.Enable()
		}
	}
	if remove == nil {
		return
	}
	if len(profiles) <= 1 || strings.EqualFold(strings.TrimSpace(selected), autoquery.DefaultProfileName) {
		remove.Disable()
		return
	}
	remove.Enable()
}

func disabledAutoQueryAction(text string) *widget.Button {
	button := newQueryPrimaryActionButton(text, nil)
	button.Importance = widget.LowImportance
	button.Disable()
	return button
}

func newQueryManualDateInputs(onDate *widget.Entry, lastHours *widget.Entry, applyPreset func()) fyne.CanvasObject {
	return container.NewGridWithColumns(2,
		labeledControl("On Date", newSteppedEntry(onDate, func(delta int) bool {
			next, ok := stepDICOMDate(onDate.Text, delta)
			if !ok {
				return false
			}
			onDate.SetText(next)
			return true
		}, applyPreset)),
		labeledControl("Last Hours", newSteppedEntry(lastHours, func(delta int) bool {
			lastHours.SetText(stepPositiveInteger(lastHours.Text, delta, 1))
			return true
		}, applyPreset)),
	)
}

func newSteppedEntry(entry *widget.Entry, step func(int) bool, apply func()) fyne.CanvasObject {
	if entry == nil {
		entry = widget.NewEntry()
	}
	stepAndApply := func(delta int) {
		if step != nil && !step(delta) {
			return
		}
		if apply != nil {
			apply()
		}
	}
	down := widget.NewButtonWithIcon("", theme.ContentRemoveIcon(), func() {
		stepAndApply(-1)
	})
	down.Importance = widget.LowImportance
	up := widget.NewButtonWithIcon("", theme.ContentAddIcon(), func() {
		stepAndApply(1)
	})
	up.Importance = widget.LowImportance
	return container.NewBorder(nil, nil, nil, container.NewHBox(down, up), entry)
}

func autoQueryResultSummaryText(state *uiState) string {
	count := 0
	kind := queryRunStudy
	if state != nil {
		count = len(state.queries)
		kind = state.autoQueryLast.kind
	}
	noun := queryRunKindNoun(kind, count)
	source := autoQueryResultSourceSummaryText(state)
	return fmt.Sprintf("%d %s found\n%s", count, noun, source)
}

func autoQueryResultSourceSummaryText(state *uiState) string {
	if sourceLabels := queryResultSourceLabels(state); len(sourceLabels) > 1 {
		return fmt.Sprintf("%d sources: %s", len(sourceLabels), strings.Join(sourceLabels, ", "))
	} else if len(sourceLabels) == 1 {
		return sourceLabels[0]
	}
	sources := autoQuerySourceNodes(state)
	switch len(sources) {
	case 0:
		return "no source selected"
	case 1:
		return queryNodeSourceSummaryLabel(sources[0])
	default:
		labels := make([]string, 0, len(sources))
		for _, source := range sources {
			labels = append(labels, queryNodeShortSourceLabel(source))
		}
		return fmt.Sprintf("%d sources: %s", len(labels), strings.Join(labels, ", "))
	}
}

func queryNodeShortSourceLabel(node nodes.Node) string {
	if name := strings.TrimSpace(node.Name); name != "" {
		return name
	}
	if host := strings.TrimSpace(node.Host); host != "" && node.Port != 0 {
		return fmt.Sprintf("%s:%d", host, node.Port)
	}
	return emptyDash(node.Name)
}

func refreshAutoQueryResultSummary(state *uiState) {
	if state == nil || state.autoQueryResultSummaryLabel == nil {
		return
	}
	state.autoQueryResultSummaryLabel.SetText(autoQueryResultSummaryText(state))
}

func autoQueryRefreshInterval(mode string) time.Duration {
	switch mode {
	case autoQueryRefreshEvery30Min:
		return 30 * time.Minute
	default:
		return 0
	}
}

func autoQueryCountdownText(mode string, next time.Time, hasQuery bool, now time.Time) string {
	if autoQueryRefreshInterval(mode) <= 0 {
		return autoQueryCountdownDormant
	}
	if !hasQuery {
		return "waiting"
	}
	if next.IsZero() {
		return autoQueryCountdownDormant
	}
	remaining := next.Sub(now)
	if remaining <= 0 {
		return "now"
	}
	remaining = remaining.Round(time.Second)
	if remaining < time.Second {
		remaining = time.Second
	}
	if remaining >= time.Hour {
		hours := int(remaining / time.Hour)
		minutes := int((remaining % time.Hour) / time.Minute)
		return fmt.Sprintf("%dh %02dm", hours, minutes)
	}
	minutes := int(remaining / time.Minute)
	seconds := int((remaining % time.Minute) / time.Second)
	return fmt.Sprintf("%02d:%02d", minutes, seconds)
}

func refreshAutoQueryCountdown(state *uiState, mode string, now time.Time) {
	if state == nil || state.autoQueryCountdownLabel == nil {
		return
	}
	state.autoQueryCountdownLabel.SetText(autoQueryCountdownText(mode, state.autoQueryNextRefresh, autoQueryRefreshAvailable(state), now))
}

func stopAutoQueryRefresh(state *uiState) {
	if state == nil {
		return
	}
	if state.autoQueryRefreshCancel != nil {
		state.autoQueryRefreshCancel()
		state.autoQueryRefreshCancel = nil
	}
	state.autoQueryNextRefresh = time.Time{}
}

func scheduleAutoQueryRefresh(w fyne.Window, status *widget.Label, tables archiveTables, table *widget.Table, state *uiState, mode string) {
	scheduleAutoQueryRefreshAt(w, status, tables, table, state, mode, time.Now())
}

func scheduleAutoQueryRefreshAt(w fyne.Window, status *widget.Label, tables archiveTables, table *widget.Table, state *uiState, mode string, now time.Time) {
	if state == nil {
		return
	}
	stopAutoQueryRefresh(state)
	interval := autoQueryRefreshInterval(mode)
	if interval <= 0 || !autoQueryRefreshAvailable(state) {
		refreshAutoQueryCountdown(state, mode, now)
		return
	}
	state.autoQueryNextRefresh = now.Add(interval)
	refreshAutoQueryCountdown(state, mode, now)
	ctx, cancel := context.WithCancel(context.Background())
	state.autoQueryRefreshCancel = cancel
	go func() {
		ticker := time.NewTicker(time.Second)
		defer ticker.Stop()
		timer := time.NewTimer(interval)
		defer timer.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case tick := <-ticker.C:
				fyne.Do(func() {
					refreshAutoQueryCountdown(state, mode, tick)
				})
			case <-timer.C:
				fyne.Do(func() {
					refreshAutoQuery(w, status, tables, table, state)
					scheduleAutoQueryRefresh(w, status, tables, table, state, mode)
				})
				return
			}
		}
	}()
}

func rememberAutoQueryStudy(state *uiState, criteria query.Criteria) {
	if state == nil {
		return
	}
	state.autoQueryLast = lastQueryRequest{kind: queryRunStudy, study: criteria}
}

func rememberAutoQueryPatient(state *uiState, criteria query.PatientCriteria) {
	if state == nil {
		return
	}
	state.autoQueryLast = lastQueryRequest{kind: queryRunPatient, patient: criteria}
}

func autoQueryRefreshAvailable(state *uiState) bool {
	return state != nil && state.autoQueryLast.kind != ""
}

func refreshAutoQuery(w fyne.Window, status *widget.Label, tables archiveTables, table *widget.Table, state *uiState) {
	if !autoQueryRefreshAvailable(state) {
		if status != nil {
			status.SetText("No Auto Q/R query to refresh")
		}
		return
	}
	switch state.autoQueryLast.kind {
	case queryRunStudy:
		runStudyQueryWithSources(w, status, table, state, state.autoQueryLast.study, autoQuerySourceNodes(state), autoQueryMatchesHandler(w, status, tables, state))
	case queryRunPatient:
		runPatientQueryWithSources(w, status, table, state, state.autoQueryLast.patient, autoQuerySourceNodes(state), autoQueryMatchesHandler(w, status, tables, state))
	default:
		if status != nil {
			status.SetText("Unsupported Auto Q/R refresh")
		}
	}
}

func autoQueryStudyCriteria(field string, search string, datePreset string, modalityChecks map[string]*widget.Check, now time.Time) (query.Criteria, bool) {
	return autoQueryStudyCriteriaWithDateInputs(field, search, datePreset, "", "", modalityChecks, now)
}

func autoQueryStudyCriteriaWithDateInputs(field string, search string, datePreset string, onDate string, lastHours string, modalityChecks map[string]*widget.Check, now time.Time) (query.Criteria, bool) {
	criteria, ok := queryCriteriaWithQuickSearch(query.Criteria{
		Modality: queryModalityCriteriaText("", modalityChecks),
	}, field, search)
	if !ok {
		return query.Criteria{}, false
	}
	dateFrom, dateTo, timeFrom, timeTo, ok := queryDateTimePresetRangeWithInputs(datePreset, onDate, lastHours, now)
	if ok {
		criteria.StudyDateFrom = dateFrom
		criteria.StudyDateTo = dateTo
		criteria.StudyTimeFrom = timeFrom
		criteria.StudyTimeTo = timeTo
	}
	return criteria, true
}

func autoQueryPatientCriteria(field string, search string) (query.PatientCriteria, bool) {
	return queryPatientCriteriaWithQuickSearch(query.PatientCriteria{}, field, search)
}

func newAutoQuerySourceList(w fyne.Window, status *widget.Label, state *uiState) *widget.List {
	var list *widget.List
	list = widget.NewList(
		func() int {
			return len(autoQuerySourceRows(state))
		},
		func() fyne.CanvasObject {
			return newQuerySourceListCell()
		},
		func(id widget.ListItemID, obj fyne.CanvasObject) {
			cell := obj.(*querySourceListCell)
			configureAutoQuerySourceCell(cell, state, id, func() {
				if err := saveAutoQuerySelectedProfile(state); err != nil {
					if status != nil {
						status.SetText("Auto Q/R source save failed")
					}
					if w != nil {
						dialog.ShowError(err, w)
					}
					return
				}
				refreshArchiveChrome(state)
				refreshQueryDestination(state)
				refreshQueryResultSummary(state)
				if list != nil {
					list.Refresh()
				}
			})
		},
	)
	list.HideSeparators = true
	applyCompactSourceListRows(list, len(autoQuerySourceRows(state)))
	list.OnSelected = func(id widget.ListItemID) {
		entries := autoQuerySourceEntries(state)
		if state == nil || id < 0 || id >= len(entries) {
			list.Unselect(id)
			return
		}
		state.selectedAutoQuerySourceRow = id
		state.selectedNodeRow = entries[id].nodeIndex
		refreshArchiveChrome(state)
		refreshQueryDestination(state)
		refreshQueryResultSummary(state)
		list.Refresh()
	}
	return list
}

func newQueryTab(w fyne.Window, status *widget.Label, tables archiveTables, nodeTable *widget.Table, state *uiState) fyne.CanvasObject {
	quickSearchField := widget.NewSelect(queryQuickSearchOptions, nil)
	quickSearchField.SetSelected(queryQuickSearchPatientName)
	quickSearch := widget.NewEntry()
	quickSearch.SetPlaceHolder("Search")
	quickSearchStrip := newQueryQuickSearchFieldStrip(quickSearchField, quickSearch)
	patientName := widget.NewEntry()
	patientName.SetPlaceHolder("DOE^JOHN")
	patientID := widget.NewEntry()
	patientID.SetPlaceHolder("12345")
	studyDateFrom := widget.NewEntry()
	studyDateFrom.SetPlaceHolder("20260101")
	studyDateTo := widget.NewEntry()
	studyDateTo.SetPlaceHolder("20261231")
	studyTimeFrom := widget.NewEntry()
	studyTimeFrom.SetPlaceHolder("000000")
	studyTimeTo := widget.NewEntry()
	studyTimeTo.SetPlaceHolder("235959")
	lastHours := widget.NewEntry()
	lastHours.SetText("1")
	lastHours.SetPlaceHolder("6")
	onDate := widget.NewEntry()
	onDate.SetText(time.Now().Format("20060102"))
	onDate.SetPlaceHolder("20260604")
	var datePreset *queryDatePresetRadioGrid
	datePreset = newQueryDatePresetRadioGrid(func(preset string) {
		if queryDatePresetPreservesManualRange(preset) {
			return
		}
		from, to, timeFrom, timeTo, ok := queryDateTimePresetRangeWithInputs(preset, onDate.Text, lastHours.Text, time.Now())
		if !ok {
			return
		}
		studyDateFrom.SetText(from)
		studyDateTo.SetText(to)
		studyTimeFrom.SetText(timeFrom)
		studyTimeTo.SetText(timeTo)
	})
	lastHours.OnSubmitted = func(_ string) {
		if datePreset.Selected() == queryDatePresetLastNHours {
			datePreset.SetSelected(queryDatePresetLastNHours)
		}
	}
	onDate.OnSubmitted = func(_ string) {
		if datePreset.Selected() == queryDatePresetOn {
			datePreset.SetSelected(queryDatePresetOn)
		}
	}
	applyManualDateInput := func() {
		switch datePreset.Selected() {
		case queryDatePresetOn:
			datePreset.SetSelected(queryDatePresetOn)
		case queryDatePresetLastNHours:
			datePreset.SetSelected(queryDatePresetLastNHours)
		}
	}
	datePreset.SetSelected(queryDatePresetAny)
	studyDescription := widget.NewEntry()
	studyDescription.SetPlaceHolder("Chest CT")
	modality := widget.NewEntry()
	modality.SetPlaceHolder("CT")
	modalityChecks := newQueryModalityChecks()
	accession := widget.NewEntry()
	accession.SetPlaceHolder("ACC123")
	studyUID := widget.NewEntry()
	studyUID.SetPlaceHolder("1.2.840...")
	seriesUID := widget.NewEntry()
	seriesUID.SetPlaceHolder("1.2.840...")
	sopUID := widget.NewEntry()
	sopUID.SetPlaceHolder("1.2.840...")
	sopClassUID := widget.NewEntry()
	sopClassUID.SetPlaceHolder("1.2.840.10008.5.1.4.1.1.2")
	instanceNumber := widget.NewEntry()
	instanceNumber.SetPlaceHolder("1")
	maxResults := widget.NewEntry()
	maxResults.SetPlaceHolder("0")
	refreshMode := widget.NewSelect(queryRefreshModeOptions, nil)
	refreshMode.SetSelected(queryRefreshModeDont)
	autoRetrieve := newQueryAutoRetrieveCheck(state)
	autoRetrieveSettings := disabledAutoQueryAction(autoQuerySettingsButtonText)
	keepOnTop := newQueryKeepOnTopCheck(state)

	var retrieveButton *widget.Button
	syncRetrieveButton := func() {
		syncQueryRetrieveButton(retrieveButton, state)
	}
	syncQuerySelection := func() {
		syncRetrieveButton()
		refreshQuerySelectedDetails(state)
	}
	queryTable := newQueryTable(state, func() {
		retrieveSelectedQuery(w, status, tables, state)
	}, syncQuerySelection)
	runButton := newQueryPrimaryActionButton(queryActionLabelQuery, func() {
		max, err := parseOptionalMaxResults(maxResults.Text)
		if err != nil {
			status.SetText("Query failed")
			dialog.ShowError(err, w)
			return
		}
		criteria, ok := queryCriteriaWithQuickSearch(query.Criteria{
			PatientName:      patientName.Text,
			PatientID:        patientID.Text,
			StudyDateFrom:    studyDateFrom.Text,
			StudyDateTo:      studyDateTo.Text,
			StudyTimeFrom:    studyTimeFrom.Text,
			StudyTimeTo:      studyTimeTo.Text,
			StudyDescription: studyDescription.Text,
			AccessionNumber:  accession.Text,
			Modality:         queryModalityCriteriaText(modality.Text, modalityChecks),
			StudyInstanceUID: studyUID.Text,
			MaxResults:       max,
		}, quickSearchField.Selected, quickSearch.Text)
		if !ok {
			status.SetText("Query failed")
			dialog.ShowError(fmt.Errorf("unsupported search field %q", quickSearchField.Selected), w)
			return
		}
		rememberLastStudyQuery(state, criteria)
		runStudyQuery(w, status, queryTable, state, criteria)
		scheduleQueryRefresh(w, status, queryTable, state, refreshMode.Selected)
	})
	if state != nil {
		state.queryRunShortcutAction = func() {
			if runButton.OnTapped != nil {
				runButton.OnTapped()
			}
		}
	}
	runPatientButton := newQueryPrimaryActionButton(queryActionLabelPatient, func() {
		max, err := parseOptionalMaxResults(maxResults.Text)
		if err != nil {
			status.SetText("Patient query failed")
			dialog.ShowError(err, w)
			return
		}
		criteria, ok := queryPatientCriteriaWithQuickSearch(query.PatientCriteria{
			PatientName: patientName.Text,
			PatientID:   patientID.Text,
			MaxResults:  max,
		}, quickSearchField.Selected, quickSearch.Text)
		if !ok {
			status.SetText("Patient query failed")
			dialog.ShowError(fmt.Errorf("unsupported Patient Root search field %q", quickSearchField.Selected), w)
			return
		}
		rememberLastPatientQuery(state, criteria)
		runPatientQuery(w, status, queryTable, state, criteria)
		scheduleQueryRefresh(w, status, queryTable, state, refreshMode.Selected)
	})
	runSeriesButton := widget.NewButtonWithIcon(queryActionLabelSeries, theme.MediaPlayIcon(), func() {
		max, err := parseOptionalMaxResults(maxResults.Text)
		if err != nil {
			status.SetText("Query failed")
			dialog.ShowError(err, w)
			return
		}
		criteria, ok := querySeriesCriteriaWithQuickSearch(query.SeriesCriteria{
			PatientName:       patientName.Text,
			PatientID:         patientID.Text,
			StudyDateFrom:     studyDateFrom.Text,
			StudyDateTo:       studyDateTo.Text,
			StudyInstanceUID:  studyUID.Text,
			SeriesInstanceUID: seriesUID.Text,
			Modality:          queryModalityCriteriaText(modality.Text, modalityChecks),
			SeriesDescription: studyDescription.Text,
			MaxResults:        max,
		}, quickSearchField.Selected, quickSearch.Text)
		if !ok {
			status.SetText("Query failed")
			dialog.ShowError(fmt.Errorf("unsupported Series search field %q", quickSearchField.Selected), w)
			return
		}
		rememberLastSeriesQuery(state, criteria)
		runSeriesQuery(w, status, queryTable, state, criteria)
		scheduleQueryRefresh(w, status, queryTable, state, refreshMode.Selected)
	})
	runSeriesButton.Importance = widget.LowImportance
	runImageButton := widget.NewButtonWithIcon(queryActionLabelImages, theme.MediaPlayIcon(), func() {
		max, err := parseOptionalMaxResults(maxResults.Text)
		if err != nil {
			status.SetText("Query failed")
			dialog.ShowError(err, w)
			return
		}
		criteria, ok := queryImageCriteriaWithQuickSearch(query.ImageCriteria{
			PatientName:       patientName.Text,
			PatientID:         patientID.Text,
			StudyDateFrom:     studyDateFrom.Text,
			StudyDateTo:       studyDateTo.Text,
			StudyInstanceUID:  studyUID.Text,
			SeriesInstanceUID: seriesUID.Text,
			SOPInstanceUID:    sopUID.Text,
			SOPClassUID:       sopClassUID.Text,
			Modality:          queryModalityCriteriaText(modality.Text, modalityChecks),
			InstanceNumber:    instanceNumber.Text,
			MaxResults:        max,
		}, quickSearchField.Selected, quickSearch.Text)
		if !ok {
			status.SetText("Query failed")
			dialog.ShowError(fmt.Errorf("unsupported Image search field %q", quickSearchField.Selected), w)
			return
		}
		rememberLastImageQuery(state, criteria)
		runImageQuery(w, status, queryTable, state, criteria)
		scheduleQueryRefresh(w, status, queryTable, state, refreshMode.Selected)
	})
	runImageButton.Importance = widget.LowImportance
	refreshButton := newQueryRefreshButton(func() {
		refreshLastQuery(w, status, queryTable, state)
		scheduleQueryRefresh(w, status, queryTable, state, refreshMode.Selected)
	})
	retrieveButton = newQueryPrimaryActionButton(queryActionLabelRetrieve, func() {
		retrieveSelectedQuery(w, status, tables, state)
	})
	syncQueryRetrieveButton(retrieveButton, state)
	verifyButton := newQueryPrimaryActionButton(queryActionLabelVerify, func() {
		verifySelectedNode(w, status, nodeTable, state)
	})
	submitStudyQuery := func() {
		if runButton != nil && runButton.OnTapped != nil {
			runButton.OnTapped()
		}
	}
	showQuickSearchFieldMenu := func(anchor fyne.CanvasObject) {
		showQueryQuickSearchFieldMenu(anchor, quickSearchField.Selected, func(field string) {
			quickSearchField.SetSelected(field)
		})
	}
	querySearchBar := workbenchStrip(newQuerySearchBar(quickSearch, submitStudyQuery, showQuickSearchFieldMenu))
	destinationSelect := newQueryMoveDestinationEntry(state)
	state.queryMoveDestinationSelect = destinationSelect
	state.queryDestinationLabel = widget.NewLabel(queryDestinationText(state))
	state.queryDestinationLabel.Wrapping = fyne.TextTruncate
	state.queryResultSummaryLabel = widget.NewLabel(queryResultSummaryText(state))
	state.queryResultSummaryLabel.Wrapping = fyne.TextTruncate
	state.queryResultSummaryLabel.Alignment = fyne.TextAlignTrailing
	state.queryCountdownLabel = widget.NewLabel(queryCountdownText(refreshMode.Selected, time.Time{}, queryRefreshAvailable(state), time.Now()))
	refreshMode.OnChanged = func(mode string) {
		scheduleQueryRefresh(w, status, queryTable, state, mode)
	}
	selectedDetails := widget.NewAccordion(widget.NewAccordionItem("Selected Result Details", newQuerySelectedDetailsPanel(state, status)))
	selectedDetails.CloseAll()
	sourceList := newQuerySourceList(state)
	state.querySourceHistoryLabel = compactWorkbenchLabel()
	state.querySourceHistoryLabel.SetText(sourceStatusHistoryText(state))
	sourceHistory := widget.NewAccordion(widget.NewAccordionItem("Source Status History", state.querySourceHistoryLabel))
	sourceHistory.CloseAll()
	refreshSources := func() {
		refreshArchiveChrome(state)
		refreshQueryDestination(state)
		refreshQueryResultSummary(state)
		refreshQuerySourceList(state)
		nodeTable.Refresh()
	}
	moveSource := func(delta int) {
		changed, err := moveQuerySource(state, delta)
		if err != nil {
			status.SetText("Source order update failed")
			dialog.ShowError(err, w)
			return
		}
		if changed {
			refreshSources()
			status.SetText("Updated source priority")
		}
	}
	moveUpButton := widget.NewButtonWithIcon("", theme.MoveUpIcon(), func() {
		moveSource(-1)
	})
	moveUpButton.Importance = widget.LowImportance
	moveDownButton := widget.NewButtonWithIcon("", theme.MoveDownIcon(), func() {
		moveSource(1)
	})
	moveDownButton.Importance = widget.LowImportance
	sourceHeader := newDicomNodesHeader(container.NewHBox(moveUpButton, moveDownButton))
	sourcePanel := newDicomNodesSourcePanel(sourceHeader, container.NewBorder(newQuerySourceColumnHeader(), nil, nil, nil, sourceList))
	advancedCriteria := newQueryAdvancedCriteria(queryAdvancedCriteriaEntries{
		patientName:      patientName,
		patientID:        patientID,
		studyDateFrom:    studyDateFrom,
		studyDateTo:      studyDateTo,
		studyTimeFrom:    studyTimeFrom,
		studyTimeTo:      studyTimeTo,
		accession:        accession,
		studyUID:         studyUID,
		seriesUID:        seriesUID,
		sopUID:           sopUID,
		sopClassUID:      sopClassUID,
		instanceNumber:   instanceNumber,
		studyDescription: studyDescription,
		modality:         modality,
		maxResults:       maxResults,
		seriesButton:     runSeriesButton,
		imagesButton:     runImageButton,
		sourceHistory:    sourceHistory,
	})

	refreshCluster := workbenchStrip(container.NewVBox(
		container.NewHBox(queryRefreshCadenceSlot(refreshMode), queryRefreshCountdownSlot(state.queryCountdownLabel), queryRefreshButtonSlot(refreshButton)),
		container.NewHBox(queryAutoRetrieveSlot(autoRetrieve), queryAutoRetrieveSettingsSlot(autoRetrieveSettings)),
	))
	filters := container.NewHBox(
		sourcePanel,
		workbenchPanelSlot("Date", container.NewVBox(datePreset.CanvasObject(), newQueryManualDateInputs(onDate, lastHours, applyManualDateInput)), queryDateFilterPanelMinWidth),
		workbenchPanelSlot("Modalities", queryModalityGrid(modalityChecks), queryModalityFilterPanelMinWidth),
	)
	criteria := container.NewVBox(
		workbenchWindowTitle(queryWorkspaceTitle),
		quickSearchStrip,
		querySearchBar,
		filters,
		container.NewBorder(
			nil,
			nil,
			newQueryPrimaryActionStrip(queryRetrieveDestinationSlot(labeledControl("Retrieve to:", destinationSelect)), runButton, runPatientButton, retrieveButton, verifyButton),
			refreshCluster,
			state.queryDestinationLabel,
		),
		advancedCriteria,
	)
	footer := newQueryFooter(keepOnTop, state.queryResultSummaryLabel)
	results := container.NewBorder(nil, selectedDetails, nil, nil, container.NewStack(queryTable))
	return container.NewBorder(nil, footer, nil, nil, newQueryWorkspace(criteria, results))
}

func newQueryDateModalityPanel(datePanel fyne.CanvasObject, modalityPanel fyne.CanvasObject) fyne.CanvasObject {
	return container.NewHBox(
		workbenchPanelSlot("Date", datePanel, queryDateFilterPanelMinWidth),
		workbenchPanelSlot("Modalities", modalityPanel, queryModalityFilterPanelMinWidth),
	)
}

func newQueryPrimaryActionStrip(objects ...fyne.CanvasObject) fyne.CanvasObject {
	return container.NewStack(
		canvas.NewRectangle(archiveHeaderRowColor),
		newCompactTableCellContent(container.NewHBox(queryPrimaryActionStripObjects(objects)...)),
		newTableColumnDividerLayer(),
		newTableRowDividerLayer(),
	)
}

func queryRetrieveDestinationSlot(destination fyne.CanvasObject) fyne.CanvasObject {
	return container.NewGridWrap(fyne.NewSize(queryRetrieveDestinationSlotWidth, destination.MinSize().Height), destination)
}

func newQueryPrimaryActionButton(text string, tapped func()) *widget.Button {
	// High importance renders a clearly filled push button that stands out from
	// the dark action strip; low/medium importance read as flat text on it.
	button := widget.NewButton(text, tapped)
	button.Importance = widget.HighImportance
	return button
}

func queryPrimaryActionStripObjects(objects []fyne.CanvasObject) []fyne.CanvasObject {
	out := make([]fyne.CanvasObject, 0, len(objects))
	for _, object := range objects {
		if _, ok := object.(*widget.Button); ok {
			size := object.MinSize()
			if size.Width < queryPrimaryActionButtonMinWidth {
				size.Width = queryPrimaryActionButtonMinWidth
			}
			object = container.NewGridWrap(size, object)
		}
		out = append(out, object)
	}
	return out
}

func newQueryFooter(keepOnTop fyne.CanvasObject, summary fyne.CanvasObject) fyne.CanvasObject {
	return workbenchStrip(container.NewBorder(
		nil,
		nil,
		keepOnTop,
		summary,
		nil,
	))
}

func newAutoQueryTitleBar(title fyne.CanvasObject, profileControls fyne.CanvasObject) fyne.CanvasObject {
	return workbenchStrip(container.NewBorder(
		nil,
		nil,
		title,
		nil,
		container.NewCenter(profileControls),
	))
}

func newAutoQueryFooter(summary fyne.CanvasObject) fyne.CanvasObject {
	return workbenchStrip(container.NewBorder(
		nil,
		nil,
		nil,
		summary,
		nil,
	))
}

const queryAdvancedCriteriaTitle = "Advanced Criteria"

type queryAdvancedCriteriaEntries struct {
	patientName      *widget.Entry
	patientID        *widget.Entry
	studyDateFrom    *widget.Entry
	studyDateTo      *widget.Entry
	studyTimeFrom    *widget.Entry
	studyTimeTo      *widget.Entry
	accession        *widget.Entry
	studyUID         *widget.Entry
	seriesUID        *widget.Entry
	sopUID           *widget.Entry
	sopClassUID      *widget.Entry
	instanceNumber   *widget.Entry
	studyDescription *widget.Entry
	modality         *widget.Entry
	maxResults       *widget.Entry
	seriesButton     *widget.Button
	imagesButton     *widget.Button
	sourceHistory    fyne.CanvasObject
}

func newQueryAdvancedCriteria(entries queryAdvancedCriteriaEntries) *widget.Accordion {
	detail := container.NewVBox(
		container.NewGridWithColumns(4,
			labeledEntry("Patient Name", entries.patientName),
			labeledEntry("Patient ID", entries.patientID),
			labeledEntry("Study Date From", entries.studyDateFrom),
			labeledEntry("Study Date To", entries.studyDateTo),
		),
		container.NewGridWithColumns(4,
			labeledEntry("Study Time From", entries.studyTimeFrom),
			labeledEntry("Study Time To", entries.studyTimeTo),
			labeledEntry("Accession", entries.accession),
			labeledEntry("Max Results", entries.maxResults),
		),
		container.NewGridWithColumns(4,
			labeledEntry("Study UID", entries.studyUID),
			labeledEntry("Series UID", entries.seriesUID),
			labeledEntry("SOP UID", entries.sopUID),
			labeledEntry("SOP Class", entries.sopClassUID),
		),
		container.NewGridWithColumns(3,
			labeledEntry("Instance #", entries.instanceNumber),
			labeledEntry("Description", entries.studyDescription),
			labeledEntry("Modality Manual", entries.modality),
		),
		container.NewHBox(entries.seriesButton, entries.imagesButton),
	)
	if entries.sourceHistory != nil {
		detail.Add(entries.sourceHistory)
	}
	advanced := widget.NewAccordion(widget.NewAccordionItem(queryAdvancedCriteriaTitle, detail))
	advanced.CloseAll()
	return advanced
}

func newQuerySourceList(state *uiState) *widget.List {
	list := widget.NewList(
		func() int {
			return len(querySourceRows(state))
		},
		func() fyne.CanvasObject {
			return newQuerySourceListCell()
		},
		func(id widget.ListItemID, obj fyne.CanvasObject) {
			cell := obj.(*querySourceListCell)
			configureQuerySourceCell(cell, state, id, func() {
				refreshArchiveChrome(state)
				refreshQueryDestination(state)
				refreshQueryResultSummary(state)
				refreshQuerySourceList(state)
			})
		},
	)
	list.HideSeparators = true
	applyCompactSourceListRows(list, len(querySourceRows(state)))
	list.OnSelected = func(id widget.ListItemID) {
		if state == nil || id < 0 || id >= len(state.nodes) {
			list.Unselect(id)
			return
		}
		state.selectedNodeRow = id
		refreshArchiveChrome(state)
		refreshQueryDestination(state)
		refreshQueryResultSummary(state)
		refreshQuerySourceList(state)
	}
	state.querySourceList = list
	return list
}

func parseOptionalMaxResults(value string) (int, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return 0, nil
	}
	max, err := strconv.Atoi(value)
	if err != nil {
		return 0, fmt.Errorf("invalid max results %q", value)
	}
	if max < 0 {
		return 0, fmt.Errorf("max results must be >= 0")
	}
	return max, nil
}

func rememberLastStudyQuery(state *uiState, criteria query.Criteria) {
	if state == nil {
		return
	}
	state.lastQuery = lastQueryRequest{kind: queryRunStudy, study: criteria}
}

func rememberLastPatientQuery(state *uiState, criteria query.PatientCriteria) {
	if state == nil {
		return
	}
	state.lastQuery = lastQueryRequest{kind: queryRunPatient, patient: criteria}
}

func rememberLastSeriesQuery(state *uiState, criteria query.SeriesCriteria) {
	if state == nil {
		return
	}
	state.lastQuery = lastQueryRequest{kind: queryRunSeries, series: criteria}
}

func rememberLastImageQuery(state *uiState, criteria query.ImageCriteria) {
	if state == nil {
		return
	}
	state.lastQuery = lastQueryRequest{kind: queryRunImage, image: criteria}
}

func queryRefreshAvailable(state *uiState) bool {
	return state != nil && state.lastQuery.kind != ""
}

func queryRefreshInterval(mode string) time.Duration {
	return autoQueryRefreshInterval(mode)
}

func queryCountdownText(mode string, next time.Time, hasQuery bool, now time.Time) string {
	if queryRefreshInterval(mode) <= 0 {
		return queryCountdownDormant
	}
	if !hasQuery {
		return "Next: waiting for Query"
	}
	if next.IsZero() {
		return queryCountdownDormant
	}
	remaining := next.Sub(now)
	if remaining <= 0 {
		return "Next: now"
	}
	remaining = remaining.Round(time.Second)
	if remaining < time.Second {
		remaining = time.Second
	}
	if remaining >= time.Hour {
		hours := int(remaining / time.Hour)
		minutes := int((remaining % time.Hour) / time.Minute)
		return fmt.Sprintf("Next: %dh %02dm", hours, minutes)
	}
	minutes := int(remaining / time.Minute)
	seconds := int((remaining % time.Minute) / time.Second)
	return fmt.Sprintf("Next: %dm %02ds", minutes, seconds)
}

func refreshQueryCountdown(state *uiState, mode string, now time.Time) {
	if state == nil || state.queryCountdownLabel == nil {
		return
	}
	state.queryCountdownLabel.SetText(queryCountdownText(mode, state.queryNextRefresh, queryRefreshAvailable(state), now))
}

func stopQueryRefresh(state *uiState) {
	if state == nil {
		return
	}
	if state.queryRefreshCancel != nil {
		state.queryRefreshCancel()
		state.queryRefreshCancel = nil
	}
	state.queryNextRefresh = time.Time{}
}

func scheduleQueryRefresh(w fyne.Window, status *widget.Label, table *widget.Table, state *uiState, mode string) {
	scheduleQueryRefreshAt(w, status, table, state, mode, time.Now())
}

func scheduleQueryRefreshAt(w fyne.Window, status *widget.Label, table *widget.Table, state *uiState, mode string, now time.Time) {
	if state == nil {
		return
	}
	stopQueryRefresh(state)
	interval := queryRefreshInterval(mode)
	if interval <= 0 || !queryRefreshAvailable(state) {
		refreshQueryCountdown(state, mode, now)
		return
	}
	state.queryNextRefresh = now.Add(interval)
	refreshQueryCountdown(state, mode, now)
	ctx, cancel := context.WithCancel(context.Background())
	state.queryRefreshCancel = cancel
	go func() {
		ticker := time.NewTicker(time.Second)
		defer ticker.Stop()
		timer := time.NewTimer(interval)
		defer timer.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case tick := <-ticker.C:
				fyne.Do(func() {
					refreshQueryCountdown(state, mode, tick)
				})
			case <-timer.C:
				fyne.Do(func() {
					refreshLastQuery(w, status, table, state)
					scheduleQueryRefresh(w, status, table, state, mode)
				})
				return
			}
		}
	}()
}

func refreshLastQuery(w fyne.Window, status *widget.Label, table *widget.Table, state *uiState) {
	if !queryRefreshAvailable(state) {
		status.SetText("No query to refresh")
		return
	}
	switch state.lastQuery.kind {
	case queryRunStudy:
		runStudyQuery(w, status, table, state, state.lastQuery.study)
	case queryRunPatient:
		runPatientQuery(w, status, table, state, state.lastQuery.patient)
	case queryRunSeries:
		runSeriesQuery(w, status, table, state, state.lastQuery.series)
	case queryRunImage:
		runImageQuery(w, status, table, state, state.lastQuery.image)
	default:
		status.SetText("No query to refresh")
	}
}

type queryMatchesHandler func()

// The multi-source query runner, its source func type, and the aggregated
// failures type live in internal/core so every frontend shares one
// implementation. core reports progress through the core.QueryObserver
// interface; the GUI's observer (queryProgressCallback) marshals updates onto
// the Fyne UI thread. The alias and thin wrappers keep call sites readable.
type queryAcrossSourceFunc = core.QuerySourceFunc
type queryActivityProgressFunc func(queryActivityProgress)

func runQueryAcrossSources(ctx context.Context, sources []nodes.Node, run queryAcrossSourceFunc) (query.Result, error) {
	return core.RunQueryAcrossSources(ctx, sources, run, nil)
}

func runQueryAcrossSourcesWithProgress(ctx context.Context, sources []nodes.Node, run queryAcrossSourceFunc, onProgress queryActivityProgressFunc) (query.Result, error) {
	return core.RunQueryAcrossSources(ctx, sources, run, core.QueryObserverFunc(onProgress))
}

func querySourcesLabel(sources []nodes.Node) string {
	if len(sources) == 1 {
		return sources[0].Name
	}
	return fmt.Sprintf("%d sources", len(sources))
}

func queryFailureWithoutResults(result query.Result, err error) bool {
	if err == nil {
		return false
	}
	var sourceFailures *core.QuerySourceFailures
	if errors.As(err, &sourceFailures) && sourceFailures.Successes > 0 {
		return false
	}
	return len(result.Matches) == 0
}

func setQueryMatches(ctx context.Context, status *widget.Label, table *widget.Table, state *uiState, matches []query.Match) error {
	var catalog queryLocalCatalog
	if state != nil && state.catalog != nil {
		catalog = state.catalog
	}
	enriched, err := enrichQueryMatchesWithLocalMetadata(ctx, catalog, matches)
	if err != nil {
		if status != nil {
			status.SetText("Query local metadata lookup failed")
		}
		return err
	}
	if state == nil {
		return nil
	}
	state.queries = enriched
	clearSelectedQuery(state)
	applySavedQuerySortPreference(state)
	state.collapsedQueryGroups = retainCollapsedQueryGroups(state.collapsedQueryGroups, enriched)
	if table != nil {
		table.Refresh()
	}
	refreshQueryResultSummary(state)
	refreshAutoQueryResultSummary(state)
	return nil
}

func retainCollapsedQueryGroups(collapsed map[string]bool, matches []query.Match) map[string]bool {
	if len(collapsed) == 0 || len(matches) == 0 {
		return nil
	}
	valid := queryGroupKeysForMatches(matches)
	retained := map[string]bool{}
	for key, isCollapsed := range collapsed {
		if isCollapsed && valid[key] {
			retained[key] = true
		}
	}
	if len(retained) == 0 {
		return nil
	}
	return retained
}

func queryGroupKeysForMatches(matches []query.Match) map[string]bool {
	keys := map[string]bool{}
	patientIndices := queryPatientGroupIndices(matches)
	for key, indices := range patientIndices {
		if queryPatientGroupAvailable(matches, indices) {
			keys[key] = true
		}
	}
	studyIndices := map[string][]int{}
	for i, match := range matches {
		key := queryStudyGroupKey(match)
		if key == "" {
			continue
		}
		studyIndices[key] = append(studyIndices[key], i)
	}
	for key, indices := range studyIndices {
		if _, _, ok := queryStudyGroupMembers(matches, indices); ok {
			keys[key] = true
		}
	}
	return keys
}

func queryCompletionStatus(prefix string, sourceLabel string, result query.Result, err error) string {
	text := fmt.Sprintf("%s %s returned %d matches, final=0x%04X in %s", prefix, sourceLabel, len(result.Matches), result.FinalStatus, result.Duration.Round(time.Millisecond))
	if err != nil {
		text += "; partial failure: " + strings.ReplaceAll(strings.TrimSpace(err.Error()), "\n", "; ")
	}
	return text
}

func recordQuerySourceStatuses(state *uiState, sources []nodes.Node, err error) {
	if state == nil {
		return
	}
	var sourceFailures *core.QuerySourceFailures
	hasSourceFailures := errors.As(err, &sourceFailures)
	if err != nil && !hasSourceFailures {
		return
	}
	for _, source := range sources {
		if hasSourceFailures && sourceFailures.Failed(source) {
			recordQuerySourceStatus(state, source, querySourceFail)
			continue
		}
		recordQuerySourceStatus(state, source, querySourceOK)
	}
}

type queryRetrieveRequest struct {
	match         query.Match
	node          nodes.Node
	opts          retrieve.Options
	level         string
	label         string
	activityLabel string
}

func retrieveSelectedQuery(w fyne.Window, status *widget.Label, tables archiveTables, state *uiState) {
	match, ok := selectedQuery(state)
	if !ok {
		status.SetText("Select a query result to retrieve")
		return
	}
	request, ok := prepareQueryRetrieveRequest(status, state, match)
	if !ok {
		return
	}
	startQueryRetrieve(w, status, tables, state, request)
}

func prepareQueryRetrieveRequest(status *widget.Label, state *uiState, match query.Match) (queryRetrieveRequest, bool) {
	level, label, ok := queryRetrieveLevelAndLabel(match)
	if !ok {
		setStatusIfPresent(status, queryRetrieveValidationMessage(match))
		return queryRetrieveRequest{}, false
	}
	node, ok := nodeForQueryMatch(state, match)
	if !ok {
		setStatusIfPresent(status, "No query-enabled remote nodes configured")
		return queryRetrieveRequest{}, false
	}
	if issue := retrieveReceiverAddressIssue(state, node); issue != "" {
		setStatusIfPresent(status, issue)
		return queryRetrieveRequest{}, false
	}
	match.QueryRetrieveLevel = level
	opts := retrieveOptionsForNode(status, state, node)
	return queryRetrieveRequest{match: match, node: node, opts: opts, level: level, label: label, activityLabel: queryRetrieveActivityLabel(match, level)}, true
}

func setStatusIfPresent(status *widget.Label, text string) {
	if status != nil {
		status.SetText(text)
	}
}

func queryRetrieveLevelAndLabel(match query.Match) (string, string, bool) {
	if !queryUIDAvailable(match.StudyInstanceUID) {
		return "", "", false
	}
	level := strings.ToUpper(strings.TrimSpace(match.QueryRetrieveLevel))
	if level == "" {
		level = "STUDY"
	}
	if level == "IMAGE" || queryUIDAvailable(match.SOPInstanceUID) {
		if !queryUIDAvailable(match.SeriesInstanceUID) || !queryUIDAvailable(match.SOPInstanceUID) {
			return "", "", false
		}
		return "IMAGE", "image " + match.SOPInstanceUID, true
	}
	if level == "SERIES" || queryUIDAvailable(match.SeriesInstanceUID) {
		if !queryUIDAvailable(match.SeriesInstanceUID) {
			return "", "", false
		}
		return "SERIES", "series " + match.SeriesInstanceUID, true
	}
	return "STUDY", "study " + match.StudyInstanceUID, true
}

func queryRetrieveActivityLabel(match query.Match, level string) string {
	scope := strings.ToLower(strings.TrimSpace(level))
	switch strings.ToUpper(strings.TrimSpace(level)) {
	case "IMAGE":
		scope = "image"
	case "SERIES":
		scope = "series"
	default:
		scope = "study"
	}
	label := strings.TrimSpace(displayPatientName(match.PatientName))
	if label == "" {
		label = strings.TrimSpace(match.StudyDescription)
	}
	if label == "" {
		label = strings.TrimSpace(match.SeriesDescription)
	}
	if label == "" {
		label = strings.TrimSpace(match.SeriesNumber)
	}
	if label == "" {
		label = strings.TrimSpace(match.StudyInstanceUID)
	}
	if label == "" {
		return "1 " + scope
	}
	return "1 " + scope + " - " + label
}

func archiveSeriesRetrieveActivityLabel(study archive.Study, series archive.Series) string {
	label := archiveRetrieveClinicalLabel(study)
	if label == "" {
		label = strings.TrimSpace(series.SeriesDescription)
	}
	if label == "" && strings.TrimSpace(series.SeriesNumber) != "" {
		label = "Series " + strings.TrimSpace(series.SeriesNumber)
	}
	if label == "" {
		label = strings.TrimSpace(series.SeriesInstanceUID)
	}
	if label == "" {
		return "1 series"
	}
	return "1 series - " + label
}

func archiveImageRetrieveActivityLabel(study archive.Study, instance archive.Instance) string {
	label := archiveRetrieveClinicalLabel(study)
	if label == "" && strings.TrimSpace(instance.InstanceNumber) != "" {
		label = "Image " + strings.TrimSpace(instance.InstanceNumber)
	}
	if label == "" {
		label = strings.TrimSpace(instance.SOPInstanceUID)
	}
	if label == "" {
		return "1 image"
	}
	return "1 image - " + label
}

func archiveRetrieveClinicalLabel(study archive.Study) string {
	label := strings.TrimSpace(displayPatientName(study.PatientName))
	if label == "" {
		label = strings.TrimSpace(study.StudyDescription)
	}
	if label == "" {
		label = strings.TrimSpace(study.StudyInstanceUID)
	}
	return label
}

func queryRetrieveValidationMessage(match query.Match) string {
	if !queryUIDAvailable(match.StudyInstanceUID) {
		return "Selected query result has no Study Instance UID"
	}
	level := strings.ToUpper(strings.TrimSpace(match.QueryRetrieveLevel))
	if level == "IMAGE" || queryUIDAvailable(match.SOPInstanceUID) {
		if !queryUIDAvailable(match.SeriesInstanceUID) {
			return "Selected query result has no Series Instance UID"
		}
		return "Selected query result has no SOP Instance UID"
	}
	if level == "SERIES" || queryUIDAvailable(match.SeriesInstanceUID) {
		return "Selected query result has no Series Instance UID"
	}
	return "Selected query result cannot be retrieved"
}

func startQueryRetrieve(w fyne.Window, status *widget.Label, tables archiveTables, state *uiState, request queryRetrieveRequest) {
	baseCtx, cancel := beginRetrieve(state, request.node.Name, request.activityLabel)
	ctx, timeoutCancel := withDICOMCommunicationTimeout(baseCtx, state)
	setStatusIfPresent(status, fmt.Sprintf("Retrieving %s from %s", request.label, request.node.Name))
	go func() {
		defer timeoutCancel()
		defer cancel()
		outcome, err := runQueryRetrieveRequest(ctx, state, request)
		studies, studyErr := loadStudies(context.Background(), state)
		fyne.Do(func() {
			clearActiveRetrieve(state)
			if err != nil {
				if errors.Is(err, context.Canceled) {
					setStatusIfPresent(status, "Retrieve cancelled for "+request.node.Name)
					return
				}
				recordQueryRetrieveRowStatus(state, request.match, queryRetrieveFailureRowStatus(request))
				refreshQueryResultSummary(state)
				setStatusIfPresent(status, "Retrieve failed for "+request.node.Name)
				dialog.ShowError(err, w)
				return
			}
			if studyErr == nil {
				setStudies(state, tables, studies)
			}
			recordOperation(state, ops.RetrieveSummary(outcome))
			recordQueryRetrieveRowStatus(state, request.match, queryRetrieveSuccessRowStatus(request, outcome))
			refreshQueryResultSummary(state)
			setStatusIfPresent(status, fmt.Sprintf(
				"%s %s: final=0x%04X completed %d failed %d warnings %d stored %d in %s",
				retrieveMethodName(outcome),
				request.node.Name,
				outcome.FinalStatus,
				outcome.Completed,
				outcome.Failed,
				outcome.Warnings,
				outcome.Stored,
				outcome.Duration.Round(time.Millisecond),
			))
			if studyErr != nil {
				dialog.ShowError(studyErr, w)
			}
		})
	}()
}

func queryRetrieveSuccessRowStatus(request queryRetrieveRequest, outcome retrieve.Outcome) string {
	return fmt.Sprintf(
		"retrieved %s from %s (%s final=0x%04X stored %d failed %d)",
		request.label,
		request.node.Name,
		retrieveMethodName(outcome),
		outcome.FinalStatus,
		outcome.Stored,
		outcome.Failed,
	)
}

func queryRetrieveFailureRowStatus(request queryRetrieveRequest) string {
	return fmt.Sprintf("retrieve failed for %s from %s", request.label, request.node.Name)
}

func runQueryRetrieveRequest(ctx context.Context, state *uiState, request queryRetrieveRequest) (retrieve.Outcome, error) {
	switch request.level {
	case "IMAGE":
		return retrieve.RetrieveImage(ctx, state.catalog, request.node, request.match.StudyInstanceUID, request.match.SeriesInstanceUID, request.match.SOPInstanceUID, request.opts)
	case "SERIES":
		return retrieve.RetrieveSeries(ctx, state.catalog, request.node, request.match.StudyInstanceUID, request.match.SeriesInstanceUID, request.opts)
	default:
		return retrieve.RetrieveStudy(ctx, state.catalog, request.node, request.match.StudyInstanceUID, request.opts)
	}
}

func runAutoQueryAutoRetrieve(w fyne.Window, status *widget.Label, tables archiveTables, state *uiState, candidates []query.Match) {
	if len(candidates) == 0 {
		setStatusIfPresent(status, "No Auto Q/R retrieve candidates")
		return
	}
	requests := make([]queryRetrieveRequest, 0, len(candidates))
	for _, candidate := range candidates {
		request, ok := prepareQueryRetrieveRequest(status, state, candidate)
		if !ok {
			return
		}
		requests = append(requests, request)
	}
	baseCtx, cancel := beginRetrieve(state, "Auto Q/R")
	ctx, timeoutCancel := withDICOMCommunicationTimeout(baseCtx, state)
	setStatusIfPresent(status, fmt.Sprintf("Auto Q/R retrieving %d matches", len(requests)))
	go func() {
		defer timeoutCancel()
		defer cancel()
		var outcomes []retrieve.Outcome
		for i, request := range requests {
			index := i + 1
			fyne.Do(func() {
				setStatusIfPresent(status, fmt.Sprintf("Auto Q/R retrieving %d/%d %s from %s", index, len(requests), request.label, request.node.Name))
			})
			outcome, err := runQueryRetrieveRequest(ctx, state, request)
			if err != nil {
				fyne.Do(func() {
					clearActiveRetrieve(state)
					if errors.Is(err, context.Canceled) {
						setStatusIfPresent(status, "Auto Q/R retrieve cancelled")
						return
					}
					setStatusIfPresent(status, "Auto Q/R retrieve failed for "+request.node.Name)
					dialog.ShowError(err, w)
				})
				return
			}
			outcomes = append(outcomes, outcome)
		}
		studies, studyErr := loadStudies(context.Background(), state)
		fyne.Do(func() {
			clearActiveRetrieve(state)
			if studyErr == nil {
				setStudies(state, tables, studies)
			}
			for _, outcome := range outcomes {
				recordOperation(state, ops.RetrieveSummary(outcome))
			}
			setStatusIfPresent(status, autoQueryRetrieveBatchStatus(outcomes))
			if studyErr != nil {
				dialog.ShowError(studyErr, w)
			}
		})
	}()
}

func autoQueryRetrieveBatchStatus(outcomes []retrieve.Outcome) string {
	var completed, failed, warnings int
	var stored int64
	var duration time.Duration
	for _, outcome := range outcomes {
		completed += int(outcome.Completed)
		failed += int(outcome.Failed)
		warnings += int(outcome.Warnings)
		stored += outcome.Stored
		duration += outcome.Duration
	}
	return fmt.Sprintf(
		"Auto Q/R retrieve completed %d requests: completed %d failed %d warnings %d stored %d in %s",
		len(outcomes),
		completed,
		failed,
		warnings,
		stored,
		duration.Round(time.Millisecond),
	)
}

func runPatientQuery(w fyne.Window, status *widget.Label, table *widget.Table, state *uiState, criteria query.PatientCriteria) {
	runPatientQueryWithSources(w, status, table, state, criteria, querySourceNodes(state), nil)
}

func runPatientQueryWithSources(w fyne.Window, status *widget.Label, table *widget.Table, state *uiState, criteria query.PatientCriteria, sources []nodes.Node, afterMatches queryMatchesHandler) {
	if len(sources) == 0 {
		status.SetText("No query-enabled remote nodes configured")
		return
	}
	callingAE := localAETitle(state)
	sourceLabel := querySourcesLabel(sources)
	beginQueryActivity(state, "Patient C-FIND "+sourceLabel)
	status.SetText("Querying patients on " + sourceLabel)
	go func() {
		ctx, cancel := withDICOMCommunicationTimeout(context.Background(), state)
		defer cancel()
		result, err := runQueryAcrossSourcesWithProgress(ctx, sources, func(ctx context.Context, node nodes.Node) (query.Result, error) {
			return query.PatientRootFind(ctx, node, criteria, callingAE)
		}, queryProgressCallback(state))
		fyne.Do(func() {
			clearActiveQueryActivity(state)
			recordQuerySourceStatuses(state, sources, err)
			refreshQuerySourceList(state)
			if queryFailureWithoutResults(result, err) {
				status.SetText("Patient query failed for " + sourceLabel)
				dialog.ShowError(err, w)
				return
			}
			if err := setQueryMatches(context.Background(), status, table, state, result.Matches); err != nil {
				dialog.ShowError(err, w)
				return
			}
			recordOperation(state, ops.QuerySummary(result))
			status.SetText(queryCompletionStatus("Patient C-FIND", sourceLabel, result, err))
			if afterMatches != nil {
				afterMatches()
			}
		})
	}()
}

func runStudyQuery(w fyne.Window, status *widget.Label, table *widget.Table, state *uiState, criteria query.Criteria) {
	runStudyQueryWithSources(w, status, table, state, criteria, querySourceNodes(state), nil)
}

func runStudyQueryWithSources(w fyne.Window, status *widget.Label, table *widget.Table, state *uiState, criteria query.Criteria, sources []nodes.Node, afterMatches queryMatchesHandler) {
	if len(sources) == 0 {
		status.SetText("No query-enabled remote nodes configured")
		return
	}
	callingAE := localAETitle(state)
	criteriaWindows := studyQueryCriteriaWindows(criteria)
	sourceLabel := querySourcesLabel(sources)
	beginQueryActivity(state, "Study C-FIND "+sourceLabel)
	status.SetText("Querying " + sourceLabel)
	go func() {
		ctx, cancel := withDICOMCommunicationTimeout(context.Background(), state)
		defer cancel()
		result, err := runQueryAcrossSourcesWithProgress(ctx, sources, func(ctx context.Context, node nodes.Node) (query.Result, error) {
			return runStudyQueryCriteriaWindows(ctx, node, criteriaWindows, callingAE)
		}, queryProgressCallback(state))
		fyne.Do(func() {
			clearActiveQueryActivity(state)
			recordQuerySourceStatuses(state, sources, err)
			refreshQuerySourceList(state)
			if queryFailureWithoutResults(result, err) {
				status.SetText("Query failed for " + sourceLabel)
				dialog.ShowError(err, w)
				return
			}
			if err := setQueryMatches(context.Background(), status, table, state, result.Matches); err != nil {
				dialog.ShowError(err, w)
				return
			}
			recordOperation(state, ops.QuerySummary(result))
			status.SetText(queryCompletionStatus("C-FIND", sourceLabel, result, err))
			if afterMatches != nil {
				afterMatches()
			}
		})
	}()
}

func runStudyQueryCriteriaWindows(ctx context.Context, node nodes.Node, criteriaWindows []query.Criteria, callingAE string) (query.Result, error) {
	return runStudyQueryCriteriaWindowsWithFind(ctx, node, criteriaWindows, callingAE, query.StudyRootFind)
}

func runStudyQueryCriteriaWindowsWithFind(ctx context.Context, node nodes.Node, criteriaWindows []query.Criteria, callingAE string, find func(context.Context, nodes.Node, query.Criteria, string) (query.Result, error)) (query.Result, error) {
	var merged query.Result
	if find == nil {
		return merged, errors.New("study query finder is required")
	}
	for _, criteria := range criteriaWindows {
		if criteria.MaxResults > 0 {
			remaining := criteria.MaxResults - len(merged.Matches)
			if remaining <= 0 {
				break
			}
			criteria.MaxResults = remaining
		}
		result, err := find(ctx, node, criteria, callingAE)
		if err != nil {
			return query.Result{}, err
		}
		merged.Matches = append(merged.Matches, result.Matches...)
		merged.FinalStatus = result.FinalStatus
		merged.Duration += result.Duration
	}
	return merged, nil
}

func studyQueryCriteriaWindows(criteria query.Criteria) []query.Criteria {
	timeFrom := strings.TrimSpace(criteria.StudyTimeFrom)
	timeTo := strings.TrimSpace(criteria.StudyTimeTo)
	if strings.TrimSpace(criteria.StudyDateFrom) == "" ||
		strings.TrimSpace(criteria.StudyDateTo) == "" ||
		timeFrom == "" ||
		timeTo == "" {
		return []query.Criteria{criteria}
	}
	fromDay, err := time.ParseInLocation("20060102", strings.TrimSpace(criteria.StudyDateFrom), time.Local)
	if err != nil {
		return []query.Criteria{criteria}
	}
	toDay, err := time.ParseInLocation("20060102", strings.TrimSpace(criteria.StudyDateTo), time.Local)
	if err != nil || !fromDay.Before(toDay) || timeFrom <= timeTo {
		return []query.Criteria{criteria}
	}

	first := criteria
	first.StudyDateFrom = fromDay.Format("20060102")
	first.StudyDateTo = first.StudyDateFrom
	first.StudyTimeTo = "235959"
	windows := []query.Criteria{first}

	middleFrom := fromDay.AddDate(0, 0, 1)
	middleTo := toDay.AddDate(0, 0, -1)
	if !middleFrom.After(middleTo) {
		middle := criteria
		middle.StudyDateFrom = middleFrom.Format("20060102")
		middle.StudyDateTo = middleTo.Format("20060102")
		middle.StudyTimeFrom = ""
		middle.StudyTimeTo = ""
		windows = append(windows, middle)
	}

	last := criteria
	last.StudyDateFrom = toDay.Format("20060102")
	last.StudyDateTo = last.StudyDateFrom
	last.StudyTimeFrom = "000000"
	windows = append(windows, last)
	return windows
}

func runSeriesQuery(w fyne.Window, status *widget.Label, table *widget.Table, state *uiState, criteria query.SeriesCriteria) {
	sources := querySourceNodes(state)
	if len(sources) == 0 {
		status.SetText("No query-enabled remote nodes configured")
		return
	}
	callingAE := localAETitle(state)
	sourceLabel := querySourcesLabel(sources)
	beginQueryActivity(state, "Series C-FIND "+sourceLabel)
	status.SetText("Querying series on " + sourceLabel)
	go func() {
		ctx, cancel := withDICOMCommunicationTimeout(context.Background(), state)
		defer cancel()
		result, err := runQueryAcrossSourcesWithProgress(ctx, sources, func(ctx context.Context, node nodes.Node) (query.Result, error) {
			return query.StudyRootSeriesFind(ctx, node, criteria, callingAE)
		}, queryProgressCallback(state))
		fyne.Do(func() {
			clearActiveQueryActivity(state)
			recordQuerySourceStatuses(state, sources, err)
			refreshQuerySourceList(state)
			if queryFailureWithoutResults(result, err) {
				status.SetText("Series query failed for " + sourceLabel)
				dialog.ShowError(err, w)
				return
			}
			if err := setQueryMatches(context.Background(), status, table, state, result.Matches); err != nil {
				dialog.ShowError(err, w)
				return
			}
			recordOperation(state, ops.QuerySummary(result))
			status.SetText(queryCompletionStatus("Series C-FIND", sourceLabel, result, err))
		})
	}()
}

func runImageQuery(w fyne.Window, status *widget.Label, table *widget.Table, state *uiState, criteria query.ImageCriteria) {
	sources := querySourceNodes(state)
	if len(sources) == 0 {
		status.SetText("No query-enabled remote nodes configured")
		return
	}
	callingAE := localAETitle(state)
	sourceLabel := querySourcesLabel(sources)
	beginQueryActivity(state, "Image C-FIND "+sourceLabel)
	status.SetText("Querying images on " + sourceLabel)
	go func() {
		ctx, cancel := withDICOMCommunicationTimeout(context.Background(), state)
		defer cancel()
		result, err := runQueryAcrossSourcesWithProgress(ctx, sources, func(ctx context.Context, node nodes.Node) (query.Result, error) {
			return query.StudyRootImageFind(ctx, node, criteria, callingAE)
		}, queryProgressCallback(state))
		fyne.Do(func() {
			clearActiveQueryActivity(state)
			recordQuerySourceStatuses(state, sources, err)
			refreshQuerySourceList(state)
			if queryFailureWithoutResults(result, err) {
				status.SetText("Image query failed for " + sourceLabel)
				dialog.ShowError(err, w)
				return
			}
			if err := setQueryMatches(context.Background(), status, table, state, result.Matches); err != nil {
				dialog.ShowError(err, w)
				return
			}
			recordOperation(state, ops.QuerySummary(result))
			status.SetText(queryCompletionStatus("Image C-FIND", sourceLabel, result, err))
		})
	}()
}

func selectedQuery(state *uiState) (query.Match, bool) {
	if state == nil {
		return query.Match{}, false
	}
	if state.selectedQueryVirtual {
		return state.selectedQueryVirtualMatch, true
	}
	if len(state.queries) == 0 {
		return query.Match{}, false
	}
	row := state.selectedQueryRow
	if row < 0 || row >= len(state.queries) {
		return query.Match{}, false
	}
	return state.queries[row], true
}

const (
	queryTableColumnPatient = iota
	queryRetrieveColumn
	queryTableColumnModality
	queryTableColumnImages
	queryTableColumnStudyDate
	queryTableColumnTime
	queryTableColumnDescription
	queryTableColumnPatientID
	queryTableColumnDOB
	queryTableColumnLocalComments
	queryTableColumnServerComments
	queryTableColumnLocalState
	queryTableColumnAccession
	queryTableColumnReferrer
	queryTableColumnInstitution
	queryTableColumnStudyStatus
	queryTableColumnSeriesNumber
	queryTableColumnInstanceNumber
	queryTableColumnSource
	queryTableColumnStatus
	queryTableColumnLevel
	queryTableColumnStudyUID
	queryTableColumnSeriesUID
	queryTableColumnSOPClass
	queryTableColumnSOPUID
)

type queryTableRowKind int

const (
	queryTableRowMatch queryTableRowKind = iota
	queryTableRowStudyGroup
	queryTableRowPatientGroup
)

type queryTableRow struct {
	kind       queryTableRowKind
	key        string
	match      query.Match
	queryIndex int
	depth      int
	expanded   bool
	childCount int
}

func newQueryTable(state *uiState, onRetrieve func(), onSelectionChanged ...func()) *widget.Table {
	return newQueryTableWithColumns(state, queryTableColumns(), onRetrieve, onSelectionChanged...)
}

func newAutoQueryTable(state *uiState, onRetrieve func(), onSelectionChanged ...func()) *widget.Table {
	return newQueryTableWithColumnsAndWidths(state, autoQueryTableColumns(), autoQueryTableColumnWidths(), onRetrieve, onSelectionChanged...)
}

func newQueryTableWithColumns(state *uiState, columns []int, onRetrieve func(), onSelectionChanged ...func()) *widget.Table {
	return newQueryTableWithColumnsAndWidths(state, columns, queryTableColumnWidthsForColumns(columns), onRetrieve, onSelectionChanged...)
}

func newQueryTableWithColumnsAndWidths(state *uiState, columns []int, widths []float32, onRetrieve func(), onSelectionChanged ...func()) *widget.Table {
	headers := queryTableHeadersForColumns(columns)
	var table *widget.Table
	notifySelectionChanged := func() {
		for _, callback := range onSelectionChanged {
			if callback != nil {
				callback()
			}
		}
	}
	selectQueryCell := func(id widget.TableCellID) {
		if id.Col < 0 || id.Col >= len(columns) {
			return
		}
		col := columns[id.Col]
		if id.Row == 0 {
			if applyQuerySort(state, col) && table != nil {
				table.Refresh()
			}
			return
		}
		rows := queryTableRows(state)
		rowIndex := id.Row - 1
		retrieve := col == queryRetrieveColumn
		if rowIndex < 0 || rowIndex >= len(rows) {
			return
		}
		row := rows[rowIndex]
		if retrieve && queryRowCanRetrieve(row) {
			selectQueryRow(state, row)
			if table != nil {
				table.Refresh()
			}
			notifySelectionChanged()
			if onRetrieve != nil {
				onRetrieve()
			}
			return
		}
		if toggleQueryGroupRow(state, row) {
			clearSelectedQuery(state)
			if table != nil {
				table.Refresh()
			}
			notifySelectionChanged()
			return
		}
		if row.queryIndex < 0 || row.queryIndex >= len(state.queries) {
			return
		}
		selectQueryRow(state, row)
		if table != nil {
			table.Refresh()
		}
		notifySelectionChanged()
	}
	table = widget.NewTable(
		func() (int, int) {
			return len(queryTableRows(state)) + 1, len(headers)
		},
		func() fyne.CanvasObject {
			return newQueryTableCell()
		},
		func(id widget.TableCellID, obj fyne.CanvasObject) {
			cell := obj.(*queryTableCell)
			if id.Col < 0 || id.Col >= len(columns) {
				applyQueryTableCell(cell, id.Row, id.Col, "", id.Row == 0, false, false, 0, "")
				return
			}
			col := columns[id.Col]
			if id.Row == 0 {
				applyQueryHeaderTableCell(cell, col, queryHeaderLabel(state, col, headers[id.Col]), state)
				return
			}
			rows := queryTableRows(state)
			if id.Row-1 < 0 || id.Row-1 >= len(rows) {
				applyQueryTableCell(cell, id.Row, col, "", false, false, false, 0, "")
				return
			}
			row := rows[id.Row-1]
			match := row.match
			selected := queryRowSelected(state, row)
			retrieveAction := col == queryRetrieveColumn && queryRowCanRetrieve(row)
			text := queryRowCell(row, col)
			localState := match.LocalState
			if col == queryTableColumnLocalState {
				text, localState = queryRowLocalStateCell(state, row)
			} else if col == queryTableColumnPatient {
				_, localState = queryRowLocalStateCell(state, row)
			}
			applyQueryTableCell(cell, id.Row, col, text, false, selected, retrieveAction, match.Status, localState)
			if retrieveAction {
				cellID := id
				cell.retrieveButton.OnTapped = func() {
					selectQueryCell(cellID)
				}
			}
		},
	)
	clearSelectedQuery(state)
	table.OnSelected = selectQueryCell
	for col, width := range widths {
		table.SetColumnWidth(col, width)
	}
	applyQueryTableRows(table)
	return table
}

func syncQueryRetrieveButton(button *widget.Button, state *uiState) {
	if button == nil {
		return
	}
	if queryRetrieveActionAvailable(state) {
		button.Enable()
		return
	}
	button.Disable()
}

func queryRetrieveActionAvailable(state *uiState) bool {
	match, ok := selectedQuery(state)
	return ok && queryMatchCanRetrieve(match)
}

func clearSelectedQuery(state *uiState) {
	if state == nil {
		return
	}
	state.selectedQueryRow = -1
	state.selectedQueryVirtual = false
	state.selectedQueryVirtualMatch = query.Match{}
}

func selectQueryRow(state *uiState, row queryTableRow) {
	if state == nil {
		return
	}
	state.selectedQueryRow = row.queryIndex
	if row.kind == queryTableRowStudyGroup {
		state.selectedQueryVirtual = true
		state.selectedQueryVirtualMatch = row.match
		return
	}
	state.selectedQueryVirtual = false
	state.selectedQueryVirtualMatch = query.Match{}
}

func queryRowSelected(state *uiState, row queryTableRow) bool {
	if state == nil {
		return false
	}
	if row.kind == queryTableRowStudyGroup && state.selectedQueryVirtual {
		return autoQueryRetrieveCandidateKey(row.match) == autoQueryRetrieveCandidateKey(state.selectedQueryVirtualMatch)
	}
	return row.kind == queryTableRowMatch && !state.selectedQueryVirtual && row.queryIndex == state.selectedQueryRow
}

func queryRowCanRetrieve(row queryTableRow) bool {
	if row.kind == queryTableRowPatientGroup {
		return false
	}
	return queryMatchCanRetrieve(row.match)
}

func queryTableRows(state *uiState) []queryTableRow {
	if state == nil || len(state.queries) == 0 {
		return nil
	}
	patientIndices := queryPatientGroupIndices(state.queries)
	patientGroupKeys := queryPatientGroupKeys(state.queries, patientIndices)
	processed := map[int]bool{}
	var rows []queryTableRow
	for i, match := range state.queries {
		if processed[i] {
			continue
		}
		patientKey := queryPatientGroupKey(match)
		if patientKey != "" && patientGroupKeys[patientKey] {
			groupIndices := patientIndices[patientKey]
			for _, index := range groupIndices {
				processed[index] = true
			}
			expanded := !state.collapsedQueryGroups[patientKey]
			parent := queryPatientGroupMatch(state.queries[groupIndices[0]])
			parent = queryApplyGroupLocalState(parent, state, groupIndices)
			rows = append(rows, queryTableRow{
				kind:       queryTableRowPatientGroup,
				key:        patientKey,
				match:      parent,
				queryIndex: groupIndices[0],
				expanded:   expanded,
				childCount: len(groupIndices),
			})
			if expanded {
				rows = append(rows, queryTableRowsForIndices(state, groupIndices, 1)...)
			}
			continue
		}
		indices := queryUngroupedStudyIndices(state.queries, i, processed, patientGroupKeys)
		for _, index := range indices {
			processed[index] = true
		}
		rows = append(rows, queryTableRowsForIndices(state, indices, 0)...)
	}
	return rows
}

func queryTableRowsForIndices(state *uiState, indices []int, depth int) []queryTableRow {
	indicesByStudy := map[string][]int{}
	for _, i := range indices {
		match := state.queries[i]
		key := queryStudyGroupKey(match)
		if key == "" {
			continue
		}
		indicesByStudy[key] = append(indicesByStudy[key], i)
	}
	processed := map[int]bool{}
	var rows []queryTableRow
	for _, i := range indices {
		match := state.queries[i]
		if processed[i] {
			continue
		}
		key := queryStudyGroupKey(match)
		groupIndices := indicesByStudy[key]
		parentIndex, childIndices, ok := queryStudyGroupMembers(state.queries, groupIndices)
		if key != "" && ok {
			for _, index := range groupIndices {
				processed[index] = true
			}
			expanded := !state.collapsedQueryGroups[key]
			parent := queryStudyGroupMatch(state.queries[parentIndex])
			parent = queryApplyGroupLocalState(parent, state, groupIndices)
			rows = append(rows, queryTableRow{
				kind:       queryTableRowStudyGroup,
				key:        key,
				match:      parent,
				queryIndex: parentIndex,
				depth:      depth,
				expanded:   expanded,
				childCount: len(childIndices),
			})
			if expanded {
				for _, childIndex := range childIndices {
					rows = append(rows, queryTableRow{
						kind:       queryTableRowMatch,
						key:        key,
						match:      state.queries[childIndex],
						queryIndex: childIndex,
						depth:      depth + 1,
					})
				}
			}
			continue
		}
		rows = append(rows, queryTableRow{kind: queryTableRowMatch, match: match, queryIndex: i, depth: depth})
		processed[i] = true
	}
	return rows
}

func queryApplyGroupLocalState(parent query.Match, state *uiState, indices []int) query.Match {
	if state == nil {
		return parent
	}
	matches := state.queries
	retrieveState := ""
	for _, index := range indices {
		if index < 0 || index >= len(matches) {
			continue
		}
		match := matches[index]
		status := queryRetrieveRowStatus(state, match)
		if strings.HasPrefix(status, "retrieve failed ") {
			retrieveState = queryLocalStateRetrieveFailed
			break
		}
		if retrieveState == "" && strings.HasPrefix(status, "retrieved ") {
			retrieveState = queryLocalStateRetrieved
		}
	}
	if retrieveState != "" {
		parent.LocalState = retrieveState
	} else {
		for _, index := range indices {
			if index < 0 || index >= len(matches) {
				continue
			}
			match := matches[index]
			if queryLocalStateAvailable(parent.LocalState) {
				break
			}
			if queryLocalStateAvailable(match.LocalState) {
				parent.LocalState = match.LocalState
			}
		}
	}
	for _, index := range indices {
		if index < 0 || index >= len(matches) {
			continue
		}
		if strings.TrimSpace(parent.LocalComments) != "" {
			break
		}
		if comments := strings.TrimSpace(matches[index].LocalComments); comments != "" {
			parent.LocalComments = comments
		}
	}
	return parent
}

func queryRowLocalStateText(localState string) string {
	switch strings.TrimSpace(localState) {
	case queryLocalStatePresent:
		return "Duplicate"
	case queryLocalStateRetrieved:
		return "Retrieved"
	case queryLocalStateRetrieveFailed:
		return "Failed"
	default:
		return ""
	}
}

func queryPatientGroupIndices(matches []query.Match) map[string][]int {
	out := map[string][]int{}
	for i, match := range matches {
		key := queryPatientGroupKey(match)
		if key == "" {
			continue
		}
		out[key] = append(out[key], i)
	}
	return out
}

func queryPatientGroupKeys(matches []query.Match, indicesByPatient map[string][]int) map[string]bool {
	out := map[string]bool{}
	for key, indices := range indicesByPatient {
		if queryPatientGroupAvailable(matches, indices) {
			out[key] = true
		}
	}
	return out
}

func queryPatientGroupAvailable(matches []query.Match, indices []int) bool {
	if len(indices) < 2 {
		return false
	}
	studies := map[string]bool{}
	for _, index := range indices {
		studyUID := strings.TrimSpace(matches[index].StudyInstanceUID)
		if queryUIDAvailable(studyUID) {
			studies[studyUID] = true
		}
	}
	return len(studies) > 1
}

func queryPatientGroupKey(match query.Match) string {
	if patientID := strings.TrimSpace(match.PatientID); patientID != "" {
		return "patient-id:" + strings.ToLower(patientID)
	}
	if patientName := strings.TrimSpace(match.PatientName); patientName != "" {
		return "patient-name:" + strings.ToLower(patientName)
	}
	return ""
}

func queryPatientGroupMatch(match query.Match) query.Match {
	match.QueryRetrieveLevel = "PATIENT"
	match.StudyInstanceUID = ""
	match.StudyDate = ""
	match.StudyTime = ""
	match.ImageCount = ""
	match.StudyDescription = ""
	match.AccessionNumber = ""
	match.ReferringPhysicianName = ""
	match.InstitutionName = ""
	match.StudyStatusID = ""
	match.SeriesInstanceUID = ""
	match.Modality = ""
	match.Modalities = ""
	match.SeriesNumber = ""
	match.SeriesDescription = ""
	match.SOPClassUID = ""
	match.SOPInstanceUID = ""
	match.InstanceNumber = ""
	return match
}

func queryUngroupedStudyIndices(matches []query.Match, current int, processed map[int]bool, patientGroupKeys map[string]bool) []int {
	studyKey := queryStudyGroupKey(matches[current])
	if studyKey == "" {
		return []int{current}
	}
	var indices []int
	for i, match := range matches {
		if processed[i] {
			continue
		}
		if patientKey := queryPatientGroupKey(match); patientKey != "" && patientGroupKeys[patientKey] {
			continue
		}
		if queryStudyGroupKey(match) == studyKey {
			indices = append(indices, i)
		}
	}
	if len(indices) == 0 {
		return []int{current}
	}
	return indices
}

func queryStudyGroupKey(match query.Match) string {
	studyUID := strings.TrimSpace(match.StudyInstanceUID)
	if !queryUIDAvailable(studyUID) {
		return ""
	}
	return "study:" + studyUID
}

func queryStudyGroupMembers(matches []query.Match, indices []int) (int, []int, bool) {
	if len(indices) < 2 {
		return -1, nil, false
	}
	parentIndex := indices[0]
	childIndices := make([]int, 0, len(indices))
	for _, index := range indices {
		match := matches[index]
		if queryMatchIsSeriesOrImage(match) {
			childIndices = append(childIndices, index)
			continue
		}
		if parentIndex == indices[0] || queryMatchIsSeriesOrImage(matches[parentIndex]) {
			parentIndex = index
		}
	}
	if len(childIndices) < 2 && (parentIndex < 0 || queryMatchIsSeriesOrImage(matches[parentIndex])) {
		return -1, nil, false
	}
	if len(childIndices) == 0 {
		return -1, nil, false
	}
	return parentIndex, childIndices, true
}

func queryMatchIsSeriesOrImage(match query.Match) bool {
	level := strings.ToUpper(strings.TrimSpace(match.QueryRetrieveLevel))
	return level == "SERIES" || level == "IMAGE" || queryUIDAvailable(match.SeriesInstanceUID) || queryUIDAvailable(match.SOPInstanceUID)
}

func queryStudyGroupMatch(match query.Match) query.Match {
	match.QueryRetrieveLevel = "STUDY"
	match.SeriesInstanceUID = ""
	match.SeriesNumber = ""
	match.SeriesDescription = ""
	match.SOPClassUID = ""
	match.SOPInstanceUID = ""
	match.InstanceNumber = ""
	return match
}

func toggleQueryGroupRow(state *uiState, row queryTableRow) bool {
	if state == nil || (row.kind != queryTableRowStudyGroup && row.kind != queryTableRowPatientGroup) || row.key == "" {
		return false
	}
	if state.collapsedQueryGroups == nil {
		state.collapsedQueryGroups = map[string]bool{}
	}
	if state.collapsedQueryGroups[row.key] {
		delete(state.collapsedQueryGroups, row.key)
	} else {
		state.collapsedQueryGroups[row.key] = true
	}
	return true
}

func queryGroupCountSuffix(count int, singular string, plural string) string {
	if count <= 0 {
		return ""
	}
	label := plural
	if count == 1 {
		label = singular
	}
	return fmt.Sprintf(" (%d %s)", count, label)
}

func queryRowCell(row queryTableRow, col int) string {
	if row.kind == queryTableRowPatientGroup && col == queryTableColumnPatient {
		label := strings.TrimSpace(displayPatientName(row.match.PatientName))
		if label == "" {
			label = strings.TrimSpace(row.match.PatientID)
		}
		if label == "" {
			label = "PATIENT"
		}
		label += queryGroupCountSuffix(row.childCount, "match", "matches")
		if row.expanded {
			return "▾ " + label
		}
		return "▸ " + label
	}
	if row.kind == queryTableRowStudyGroup && col == queryTableColumnPatient {
		label := strings.TrimSpace(row.match.StudyDescription)
		if label == "" {
			label = strings.TrimSpace(compactDisplayDate(row.match.StudyDate))
		}
		if label == "" {
			label = strings.TrimSpace(row.match.StudyInstanceUID)
		}
		if label == "" {
			label = "STUDY"
		}
		label += queryGroupCountSuffix(row.childCount, "item", "items")
		indent := strings.Repeat("    ", row.depth)
		if row.expanded {
			return indent + "▾ " + label
		}
		return indent + "▸ " + label
	}
	if row.kind == queryTableRowPatientGroup && col == queryTableColumnLevel {
		if row.expanded {
			return "▾ PATIENT"
		}
		return "▸ PATIENT"
	}
	if row.kind == queryTableRowStudyGroup && col == queryTableColumnLevel {
		indent := strings.Repeat("  ", row.depth)
		if row.expanded {
			return indent + "▾ STUDY"
		}
		return indent + "▸ STUDY"
	}
	if row.depth > 0 && col == queryTableColumnPatient {
		if value := strings.TrimSpace(row.match.SeriesNumber); value != "" {
			return strings.Repeat("    ", row.depth) + value
		}
		if value := strings.TrimSpace(row.match.SeriesDescription); value != "" {
			return strings.Repeat("    ", row.depth) + value
		}
	}
	return queryCell(row.match, col)
}

func queryRowLocalStateCell(state *uiState, row queryTableRow) (string, string) {
	if status := queryRetrieveRowStatus(state, row.match); status != "" {
		if strings.HasPrefix(status, "retrieved ") {
			return "Retrieved", queryLocalStateRetrieved
		}
		if strings.HasPrefix(status, "retrieve failed ") {
			return "Failed", queryLocalStateRetrieveFailed
		}
	}
	return queryCell(row.match, queryTableColumnLocalState), row.match.LocalState
}

const querySortPreferenceKey = "queryResults"

func applyQuerySort(state *uiState, col int) bool {
	if state == nil || !queryColumnSortable(col) {
		return false
	}
	if state.querySortActive && state.querySortColumn == col {
		state.querySortDescending = !state.querySortDescending
	} else {
		state.querySortActive = true
		state.querySortColumn = col
		state.querySortDescending = false
	}
	sortQueriesByColumn(state, col, state.querySortDescending)
	persistQuerySortPreference(state)
	clearSelectedQuery(state)
	return true
}

func applySavedQuerySortPreference(state *uiState) {
	if state == nil {
		return
	}
	pref, ok := state.appConfig.UISortPreferences[querySortPreferenceKey]
	if !ok || !queryColumnSortable(pref.Column) {
		pref = appconfig.SortPreference{Column: queryTableColumnPatient}
	}
	state.querySortActive = true
	state.querySortColumn = pref.Column
	state.querySortDescending = pref.Descending
	sortQueriesByColumn(state, pref.Column, pref.Descending)
	clearSelectedQuery(state)
}

func sortQueriesByColumn(state *uiState, col int, descending bool) {
	if state == nil {
		return
	}
	sort.SliceStable(state.queries, func(i, j int) bool {
		left := querySortValue(state.queries[i], col)
		right := querySortValue(state.queries[j], col)
		if left == right {
			left = strings.ToLower(strings.TrimSpace(state.queries[i].StudyInstanceUID))
			right = strings.ToLower(strings.TrimSpace(state.queries[j].StudyInstanceUID))
		}
		if descending {
			return left > right
		}
		return left < right
	})
}

func persistQuerySortPreference(state *uiState) {
	if state == nil || state.appConfigPath == "" || !state.querySortActive || !queryColumnSortable(state.querySortColumn) {
		return
	}
	if state.appConfig.UISortPreferences == nil {
		state.appConfig.UISortPreferences = map[string]appconfig.SortPreference{}
	}
	state.appConfig.UISortPreferences[querySortPreferenceKey] = appconfig.SortPreference{
		Column:     state.querySortColumn,
		Descending: state.querySortDescending,
	}
	_ = appconfig.Save(state.appConfigPath, state.appConfig)
}

func queryColumnSortable(col int) bool {
	switch col {
	case queryRetrieveColumn, queryTableColumnLocalState:
		return false
	default:
		return col >= 0 && col < len(queryTableHeaders())
	}
}

func querySortValue(match query.Match, col int) string {
	switch col {
	case queryTableColumnDOB:
		return strings.TrimSpace(match.PatientBirthDate)
	case queryTableColumnStudyDate:
		return strings.TrimSpace(match.StudyDate) + strings.TrimSpace(match.StudyTime)
	case queryTableColumnImages:
		return numericSortValue(match.ImageCount)
	default:
		return strings.ToLower(strings.TrimSpace(queryCell(match, col)))
	}
}

func queryHeaderLabel(state *uiState, col int, label string) string {
	if state == nil {
		return label
	}
	return label
}

func queryHeaderSortGlyph(state *uiState, col int) string {
	if state == nil || !state.querySortActive || state.querySortColumn != col {
		return ""
	}
	if state.querySortDescending {
		return "▾"
	}
	return "▴"
}

func applyQueryHeaderTableCell(cell *queryTableCell, tableCol int, text string, state *uiState) {
	applyQueryTableCell(cell, 0, tableCol, text, true, false, false, 0, "")
	if cell == nil {
		return
	}
	glyph := queryHeaderSortGlyph(state, tableCol)
	if glyph == "" {
		return
	}
	cell.sortLabel.SetText(glyph)
	cell.sortLabel.Show()
	cell.sortLabel.Refresh()
}

func queryTableSelectionAction(id widget.TableCellID) (int, bool, bool) {
	if id.Row <= 0 {
		return -1, false, false
	}
	return id.Row - 1, id.Col == queryRetrieveColumn, true
}

func queryMatchCanRetrieve(match query.Match) bool {
	if !queryUIDAvailable(match.StudyInstanceUID) {
		return false
	}
	level := strings.ToUpper(strings.TrimSpace(match.QueryRetrieveLevel))
	if level == "IMAGE" || queryUIDAvailable(match.SOPInstanceUID) {
		return queryUIDAvailable(match.SeriesInstanceUID) && queryUIDAvailable(match.SOPInstanceUID)
	}
	if level == "SERIES" || queryUIDAvailable(match.SeriesInstanceUID) {
		return queryUIDAvailable(match.SeriesInstanceUID)
	}
	return true
}

func queryUIDAvailable(uid string) bool {
	uid = strings.TrimSpace(uid)
	return uid != "" && uid != "(missing)"
}

func queryTableHeaders() []string {
	return queryTableHeadersForColumns(queryTableColumns())
}

func autoQueryTableHeaders() []string {
	return autoQueryTableHeadersForColumns(autoQueryTableColumns())
}

func queryTableColumns() []int {
	return []int{queryTableColumnPatient, queryRetrieveColumn, queryTableColumnModality, queryTableColumnImages, queryTableColumnStudyDate, queryTableColumnTime, queryTableColumnDescription, queryTableColumnPatientID, queryTableColumnDOB, queryTableColumnLocalComments, queryTableColumnServerComments}
}

func autoQueryTableColumns() []int {
	return []int{queryTableColumnPatient, queryRetrieveColumn, queryTableColumnPatientID, queryTableColumnDOB, queryTableColumnDescription, queryTableColumnModality, queryTableColumnImages, queryTableColumnStudyDate, queryTableColumnTime, queryTableColumnSource, queryTableColumnAccession, queryTableColumnLocalComments, queryTableColumnServerComments, queryTableColumnLocalState, queryTableColumnReferrer, queryTableColumnInstitution, queryTableColumnStudyStatus, queryTableColumnSeriesNumber, queryTableColumnInstanceNumber, queryTableColumnStatus}
}

func queryTableHeadersForColumns(columns []int) []string {
	headers := make([]string, 0, len(columns))
	for _, col := range columns {
		headers = append(headers, queryTableHeaderForColumn(col))
	}
	return headers
}

func autoQueryTableHeadersForColumns(columns []int) []string {
	headers := make([]string, 0, len(columns))
	for _, col := range columns {
		headers = append(headers, autoQueryTableHeaderForColumn(col))
	}
	return headers
}

func autoQueryTableHeaderForColumn(col int) string {
	if col == queryTableColumnDescription {
		return "Descripti..."
	}
	return queryTableHeaderForColumn(col)
}

func queryTableHeaderForColumn(col int) string {
	switch col {
	case queryTableColumnPatient:
		return "Patient Name"
	case queryRetrieveColumn:
		return ""
	case queryTableColumnModality:
		return "Modality"
	case queryTableColumnImages:
		return "# im"
	case queryTableColumnStudyDate:
		return "Date"
	case queryTableColumnTime:
		return "Time"
	case queryTableColumnDescription:
		return "Description"
	case queryTableColumnPatientID:
		return "Patient ID"
	case queryTableColumnDOB:
		return "Date of Birth"
	case queryTableColumnLocalComments:
		return "Local Comments"
	case queryTableColumnServerComments:
		return "Server Comme..."
	case queryTableColumnLocalState:
		return "Local"
	case queryTableColumnAccession:
		return "Accession #"
	case queryTableColumnReferrer:
		return "Referrer"
	case queryTableColumnInstitution:
		return "Institution"
	case queryTableColumnStudyStatus:
		return "Study Status"
	case queryTableColumnSeriesNumber:
		return "Series #"
	case queryTableColumnInstanceNumber:
		return "Instance #"
	case queryTableColumnSource:
		return "Source"
	case queryTableColumnStatus:
		return "Status"
	default:
		return ""
	}
}

func queryTableColumnWidths() []float32 {
	return []float32{500, 54, 95, 82, 120, 104, 270, 120, 145, 170, 180, 80, 120, 170, 170, 110, 85, 90, 200, 80}
}

func autoQueryTableColumnWidths() []float32 {
	return []float32{430, 54, 160, 190, 135, 125, 105, 220, 170, 150, 150, 200, 190, 80, 170, 170, 110, 85, 90, 80}
}

func queryTableColumnWidthsForColumns(columns []int) []float32 {
	base := queryTableColumnWidths()
	widths := make([]float32, 0, len(columns))
	for _, col := range columns {
		if col >= 0 && col < len(base) {
			widths = append(widths, base[col])
			continue
		}
		widths = append(widths, 120)
	}
	return widths
}

func applyQueryTableCell(cell *queryTableCell, tableRow int, tableCol int, text string, header bool, selected bool, retrieveAction bool, status uint16, localState string) {
	if cell == nil {
		return
	}
	cell.retrieveButton.OnTapped = nil
	cell.retrieveButton.Hide()
	cell.statusDotBox.Hide()
	cell.sortLabel.Hide()
	cell.label.Show()
	cell.label.SetText(text)
	cell.label.TextStyle = fyne.TextStyle{}
	cell.label.Alignment = fyne.TextAlignLeading
	cell.label.Wrapping = fyne.TextTruncate
	if header {
		cell.label.TextStyle = fyne.TextStyle{Bold: true}
		cell.background.FillColor = archiveHeaderRowColor
		cell.background.Refresh()
		return
	}
	if queryPatientCellIsDisclosure(text, tableCol) {
		cell.label.TextStyle = fyne.TextStyle{Bold: true}
	}
	if retrieveAction {
		cell.label.SetText("")
		cell.label.Hide()
		cell.retrieveButton.Show()
	}
	if tableCol == queryTableColumnStatus {
		cell.statusDot.FillColor = queryStatusDotColor(status)
		cell.statusDot.Refresh()
		cell.statusDotBox.Show()
	}
	if tableCol == queryTableColumnLocalState && queryLocalStateAvailable(localState) {
		cell.statusDot.FillColor = queryLocalStateDotColor(localState)
		cell.statusDot.Refresh()
		cell.statusDotBox.Show()
	}
	if tableCol == queryTableColumnPatient && queryLocalStateAvailable(localState) {
		cell.statusDot.FillColor = queryLocalStateDotColor(localState)
		cell.statusDot.Refresh()
		cell.statusDotBox.Show()
	}
	if selected {
		cell.background.FillColor = archiveSelectedRowColor
		cell.background.Refresh()
		return
	}
	if retrieveAction {
		cell.background.FillColor = queryRetrieveActionRowColor
		cell.background.Refresh()
		return
	}
	if tableRow%2 == 0 {
		cell.background.FillColor = archiveEvenRowColor
	} else {
		cell.background.FillColor = archiveOddRowColor
	}
	if queryPatientCellIsDisclosure(text, tableCol) {
		cell.background.FillColor = archivePatientRowColor
	}
	cell.background.Refresh()
}

func queryPatientCellIsDisclosure(text string, tableCol int) bool {
	if tableCol != queryTableColumnPatient {
		return false
	}
	text = strings.TrimLeft(strings.TrimSpace(text), " ")
	return strings.HasPrefix(text, "▾ ") || strings.HasPrefix(text, "▸ ")
}

func queryLocalStateAvailable(localState string) bool {
	switch strings.TrimSpace(localState) {
	case queryLocalStatePresent, queryLocalStateRetrieved, queryLocalStateRetrieveFailed:
		return true
	default:
		return false
	}
}

func queryLocalStateDotColor(localState string) color.NRGBA {
	switch strings.TrimSpace(localState) {
	case queryLocalStatePresent, queryLocalStateRetrieved:
		return queryLocalStatePresentColor
	case queryLocalStateRetrieveFailed:
		return queryStatusFailColor
	default:
		return sourceStatusIdleColor
	}
}

func toggleNodeOperationalCell(state *uiState, row int, col int) (bool, error) {
	if state == nil || row < 0 || row >= len(state.nodes) {
		return false, nil
	}
	switch col {
	case nodeTableColumnEnabled:
		return setNodeOperationalCheck(state, row, col, !state.nodes[row].Enabled())
	case nodeTableColumnQuery:
		return setNodeOperationalCheck(state, row, col, !state.nodes[row].QueryEnabled())
	case nodeTableColumnSend:
		return setNodeOperationalCheck(state, row, col, !state.nodes[row].SendEnabled())
	}
	next := state.nodes[row]
	switch col {
	case nodeTableColumnRetrieve:
		next.RetrieveMethod = nextRetrieveMethod(next.RetrieveMethod)
	case nodeTableColumnTLS:
		next.UseTLS = !next.UseTLS
	default:
		return false, nil
	}
	return saveNodeAt(state, row, next)
}

func setNodeOperationalCheck(state *uiState, row int, col int, checked bool) (bool, error) {
	if state == nil || row < 0 || row >= len(state.nodes) {
		return false, nil
	}
	next := state.nodes[row]
	switch col {
	case nodeTableColumnEnabled:
		next.Disabled = !checked
	case nodeTableColumnQuery:
		next.QueryDisabled = !checked
	case nodeTableColumnSend:
		next.SendDisabled = !checked
	default:
		return false, nil
	}
	return saveNodeAt(state, row, next)
}

func setNodeRetrieveMethod(state *uiState, row int, method string) (bool, error) {
	if state == nil || row < 0 || row >= len(state.nodes) {
		return false, nil
	}
	normalized, err := nodes.NormalizeRetrieveMethod(method)
	if err != nil {
		return false, err
	}
	next := state.nodes[row]
	next.RetrieveMethod = normalized
	return saveNodeAt(state, row, next)
}

func setNodeSendSyntax(state *uiState, row int, label string) (bool, error) {
	if state == nil || row < 0 || row >= len(state.nodes) {
		return false, nil
	}
	next := state.nodes[row]
	next.SendTransferSyntax = sendSyntaxValue(label)
	return saveNodeAt(state, row, next)
}

func setNodeTLSLabel(state *uiState, row int, label string) (bool, error) {
	if state == nil || row < 0 || row >= len(state.nodes) {
		return false, nil
	}
	next := state.nodes[row]
	next.UseTLS = strings.EqualFold(strings.TrimSpace(label), "Yes")
	return saveNodeAt(state, row, next)
}

func setNodeDropdownValue(state *uiState, row int, col int, value string) (bool, error) {
	switch col {
	case nodeTableColumnTLS:
		return setNodeTLSLabel(state, row, value)
	case nodeTableColumnRetrieve:
		return setNodeRetrieveMethod(state, row, value)
	case nodeTableColumnSendSyntax:
		return setNodeSendSyntax(state, row, value)
	default:
		return false, nil
	}
}

func saveNodeAt(state *uiState, row int, next nodes.Node) (bool, error) {
	if state == nil || row < 0 || row >= len(state.nodes) {
		return false, nil
	}
	if next == state.nodes[row] {
		return false, nil
	}
	original := state.nodes[row]
	state.nodes[row] = next
	if state.nodeStore != nil {
		if err := state.nodeStore.Save(state.nodes); err != nil {
			state.nodes[row] = original
			return true, err
		}
	}
	return true, nil
}

func nextRetrieveMethod(current string) string {
	method, err := nodes.NormalizeRetrieveMethod(current)
	if err != nil {
		return ""
	}
	switch method {
	case "":
		return nodes.RetrieveMethodMove
	case nodes.RetrieveMethodMove:
		return nodes.RetrieveMethodGet
	default:
		return ""
	}
}

type nodeTableCell struct {
	widget.BaseWidget
	Container      *fyne.Container
	background     *canvas.Rectangle
	label          *widget.Label
	sortLabel      *widget.Label
	check          *widget.Check
	retrieveSelect *widget.Select
	retrieveSlot   *fyne.Container
}

func newNodeTableCell() *nodeTableCell {
	background := canvas.NewRectangle(archiveOddRowColor)
	label := widget.NewLabel("wide table cell value")
	label.Wrapping = fyne.TextTruncate
	sortLabel := widget.NewLabel("")
	sortLabel.Alignment = fyne.TextAlignTrailing
	sortLabel.TextStyle = fyne.TextStyle{Bold: true}
	sortLabel.Hide()
	check := widget.NewCheck("", nil)
	check.Hide()
	retrieveSelect := widget.NewSelect(retrieveMethodOptions(), nil)
	retrieveSelect.Hide()
	retrieveSlot := container.New(layout.NewGridWrapLayout(fyne.NewSize(nodeDropdownSlotWidth(nodeTableColumnRetrieve), retrieveSelect.MinSize().Height)), retrieveSelect)
	labelRow := container.NewBorder(nil, nil, nil, sortLabel, label)
	cell := &nodeTableCell{
		Container:      container.NewStack(background, newCompactTableCellContent(labelRow), container.NewCenter(check), newCompactTableCellContent(retrieveSlot), newTableColumnDividerLayer(), newTableRowDividerLayer()),
		background:     background,
		label:          label,
		sortLabel:      sortLabel,
		check:          check,
		retrieveSelect: retrieveSelect,
		retrieveSlot:   retrieveSlot,
	}
	cell.ExtendBaseWidget(cell)
	return cell
}

func (cell *nodeTableCell) CreateRenderer() fyne.WidgetRenderer {
	return widget.NewSimpleRenderer(cell.Container)
}

func newNodeTable(status *widget.Label, state *uiState) *widget.Table {
	headers := nodeTableHeaders()
	var table *widget.Table
	refreshAfterNodeChange := func(row int, changed bool, err error) {
		if err != nil && status != nil {
			status.SetText("Node update failed")
		} else if changed && status != nil {
			status.SetText("Updated node " + state.nodes[row].Name)
		}
		refreshArchiveChrome(state)
		refreshQueryDestination(state)
		refreshQueryResultSummary(state)
		refreshQuerySourceList(state)
		if table != nil {
			table.Refresh()
		}
	}
	selectNodeCell := func(id widget.TableCellID) {
		if id.Row <= 0 {
			if applyNodeSort(state, id.Col) && table != nil {
				table.Refresh()
			}
			return
		}
		row, ok := nodeTableNodeIndex(state, id.Row-1)
		if !ok {
			return
		}
		state.selectedNodeRow = row
		changed, err := toggleNodeOperationalCell(state, row, id.Col)
		refreshAfterNodeChange(row, changed, err)
	}
	table = widget.NewTable(
		func() (int, int) {
			return len(state.nodes) + 1, len(headers)
		},
		func() fyne.CanvasObject {
			return newNodeTableCell()
		},
		func(id widget.TableCellID, obj fyne.CanvasObject) {
			cell := obj.(*nodeTableCell)
			if id.Row == 0 {
				applyNodeHeaderTableCell(cell, id.Col, nodeHeaderLabel(state, id.Col, headers[id.Col]), state)
				return
			}
			nodeIndex, ok := nodeTableNodeIndex(state, id.Row-1)
			if !ok {
				applyNodeTableCell(cell, id.Row, id.Col, "", false, false, false, false, false, false, "")
				return
			}
			node := state.nodes[nodeIndex]
			selected := nodeIndex == state.selectedNodeRow
			checkbox, checked := nodeOperationalCheckboxState(node, id.Col)
			dropdown, dropdownValue := nodeDropdownState(node, id.Col)
			applyNodeTableCell(cell, id.Row, id.Col, nodeCell(node, id.Col), false, selected, !node.Enabled(), checkbox, checked, dropdown, dropdownValue)
			if checkbox {
				cellID := id
				cell.check.OnChanged = func(checked bool) {
					row, ok := nodeTableNodeIndex(state, cellID.Row-1)
					if !ok {
						return
					}
					state.selectedNodeRow = row
					changed, err := setNodeOperationalCheck(state, row, cellID.Col, checked)
					refreshAfterNodeChange(row, changed, err)
				}
			}
			if dropdown {
				cellID := id
				cell.retrieveSelect.OnChanged = func(value string) {
					row, ok := nodeTableNodeIndex(state, cellID.Row-1)
					if !ok {
						return
					}
					state.selectedNodeRow = row
					changed, err := setNodeDropdownValue(state, row, cellID.Col, value)
					refreshAfterNodeChange(row, changed, err)
				}
			}
		},
	)
	state.selectedNodeRow = -1
	table.OnSelected = selectNodeCell
	for col, width := range nodeTableColumnWidths() {
		table.SetColumnWidth(col, width)
	}
	applyNetworkTableRows(table)
	return table
}

const (
	nodeTableColumnEnabled = iota
	nodeTableColumnHost
	nodeTableColumnAETitle
	nodeTableColumnPort
	nodeTableColumnQuery
	nodeTableColumnRetrieve
	nodeTableColumnSend
	nodeTableColumnTLS
	nodeTableColumnName
	nodeTableColumnSendSyntax
	nodeTableColumnMoveDestination
	nodeTableColumnNotes
)

const nodeSortPreferenceKey = "networkNodes"

func applyNodeSort(state *uiState, col int) bool {
	if state == nil || !nodeColumnSortable(col) {
		return false
	}
	if state.nodeSortActive && state.nodeSortColumn == col {
		state.nodeSortDescending = !state.nodeSortDescending
	} else {
		state.nodeSortActive = true
		state.nodeSortColumn = col
		state.nodeSortDescending = false
	}
	descending := state.nodeSortDescending
	state.nodeTableRows = makeNodeTableRows(len(state.nodes))
	sortNodeTableRows(state, col, descending)
	persistNodeSortPreference(state)
	state.selectedNodeRow = -1
	return true
}

func applySavedNodeSortPreference(state *uiState) {
	if state == nil {
		return
	}
	pref, ok := state.appConfig.UISortPreferences[nodeSortPreferenceKey]
	if !ok || !nodeColumnSortable(pref.Column) {
		refreshNodeTableRows(state)
		return
	}
	state.nodeSortActive = true
	state.nodeSortColumn = pref.Column
	state.nodeSortDescending = pref.Descending
	refreshNodeTableRows(state)
}

func refreshNodeTableRows(state *uiState) {
	if state == nil {
		return
	}
	state.nodeTableRows = makeNodeTableRows(len(state.nodes))
	if state.nodeSortActive && nodeColumnSortable(state.nodeSortColumn) {
		sortNodeTableRows(state, state.nodeSortColumn, state.nodeSortDescending)
	}
}

func makeNodeTableRows(count int) []int {
	if count <= 0 {
		return nil
	}
	rows := make([]int, count)
	for i := range rows {
		rows[i] = i
	}
	return rows
}

func ensureNodeTableRows(state *uiState) []int {
	if state == nil {
		return nil
	}
	if !nodeTableRowsValid(state) {
		refreshNodeTableRows(state)
	}
	return state.nodeTableRows
}

func persistNodeSortPreference(state *uiState) {
	if state == nil || state.appConfigPath == "" || !state.nodeSortActive || !nodeColumnSortable(state.nodeSortColumn) {
		return
	}
	if state.appConfig.UISortPreferences == nil {
		state.appConfig.UISortPreferences = map[string]appconfig.SortPreference{}
	}
	state.appConfig.UISortPreferences[nodeSortPreferenceKey] = appconfig.SortPreference{
		Column:     state.nodeSortColumn,
		Descending: state.nodeSortDescending,
	}
	_ = appconfig.Save(state.appConfigPath, state.appConfig)
}

func nodeTableRowsValid(state *uiState) bool {
	if state == nil || len(state.nodeTableRows) != len(state.nodes) {
		return false
	}
	seen := make([]bool, len(state.nodes))
	for _, index := range state.nodeTableRows {
		if index < 0 || index >= len(state.nodes) || seen[index] {
			return false
		}
		seen[index] = true
	}
	return true
}

func nodeTableNodeIndex(state *uiState, row int) (int, bool) {
	rows := ensureNodeTableRows(state)
	if row < 0 || row >= len(rows) {
		return -1, false
	}
	index := rows[row]
	return index, index >= 0 && index < len(state.nodes)
}

func sortNodeTableRows(state *uiState, col int, descending bool) {
	if state == nil || len(state.nodes) == 0 {
		return
	}
	if !nodeTableRowsValid(state) {
		state.nodeTableRows = makeNodeTableRows(len(state.nodes))
	}
	sort.SliceStable(state.nodeTableRows, func(i, j int) bool {
		leftIndex := state.nodeTableRows[i]
		rightIndex := state.nodeTableRows[j]
		left := nodeSortValue(state.nodes[leftIndex], col)
		right := nodeSortValue(state.nodes[rightIndex], col)
		if left == right {
			left = nodeSortTieValue(state.nodes[leftIndex])
			right = nodeSortTieValue(state.nodes[rightIndex])
		}
		if descending {
			return left > right
		}
		return left < right
	})
}

func nodeColumnSortable(col int) bool {
	switch col {
	case nodeTableColumnEnabled, nodeTableColumnQuery, nodeTableColumnRetrieve, nodeTableColumnSend, nodeTableColumnTLS:
		return false
	default:
		return col >= 0 && col < len(nodeTableHeaders())
	}
}

func nodeSortValue(node nodes.Node, col int) string {
	if col == nodeTableColumnPort {
		return fmt.Sprintf("%05d", node.Port)
	}
	return strings.ToLower(strings.TrimSpace(nodeCell(node, col)))
}

func nodeSortTieValue(node nodes.Node) string {
	return strings.ToLower(strings.TrimSpace(node.Name + "|" + node.AETitle))
}

func nodeHeaderLabel(state *uiState, col int, label string) string {
	return label
}

func nodeHeaderSortGlyph(state *uiState, col int) string {
	if state == nil || !state.nodeSortActive || state.nodeSortColumn != col {
		return ""
	}
	if state.nodeSortDescending {
		return "▾"
	}
	return "▴"
}

func nodeOperationalCheckboxState(node nodes.Node, col int) (bool, bool) {
	switch col {
	case nodeTableColumnEnabled:
		return true, node.Enabled()
	case nodeTableColumnQuery:
		return true, node.QueryEnabled()
	case nodeTableColumnSend:
		return true, node.SendEnabled()
	default:
		return false, false
	}
}

func nodeDropdownState(node nodes.Node, col int) (bool, string) {
	switch col {
	case nodeTableColumnTLS:
		if node.UseTLS {
			return true, "Yes"
		}
		return true, "No"
	case nodeTableColumnRetrieve:
		return true, node.RetrieveMethodOrDefault()
	case nodeTableColumnSendSyntax:
		return true, sendSyntaxTableLabel(node.SendTransferSyntaxOrDefault())
	default:
		return false, ""
	}
}

func nodeDropdownSlotWidth(col int) float32 {
	switch col {
	case nodeTableColumnTLS:
		return 64
	case nodeTableColumnSendSyntax:
		return 316
	default:
		return 96
	}
}

func applyNodeTableCell(cell *nodeTableCell, tableRow int, tableCol int, text string, header bool, selected bool, disabled bool, checkbox bool, checked bool, retrieveDropdown bool, retrieveValue string) {
	if cell == nil {
		return
	}
	cell.sortLabel.Hide()
	cell.check.OnChanged = nil
	cell.check.Hide()
	cell.retrieveSelect.OnChanged = nil
	cell.retrieveSelect.Enable()
	cell.retrieveSelect.Hide()
	cell.label.Show()
	cell.label.SetText(text)
	cell.label.TextStyle = fyne.TextStyle{Italic: disabled}
	cell.label.Alignment = fyne.TextAlignLeading
	cell.label.Wrapping = fyne.TextTruncate
	if header {
		cell.label.TextStyle = fyne.TextStyle{Bold: true}
		cell.background.FillColor = archiveHeaderRowColor
		cell.background.Refresh()
		return
	}
	if checkbox {
		cell.label.SetText("")
		cell.label.Hide()
		cell.check.SetChecked(checked)
		cell.check.Show()
	}
	if retrieveDropdown {
		cell.label.SetText("")
		cell.label.Hide()
		cell.retrieveSlot.Layout = layout.NewGridWrapLayout(fyne.NewSize(nodeDropdownSlotWidth(tableCol), cell.retrieveSelect.MinSize().Height))
		switch tableCol {
		case nodeTableColumnTLS:
			cell.retrieveSelect.SetOptions([]string{"No", "Yes"})
		case nodeTableColumnSendSyntax:
			cell.retrieveSelect.SetOptions(sendSyntaxOptions())
		default:
			cell.retrieveSelect.SetOptions(retrieveMethodOptions())
		}
		cell.retrieveSelect.SetSelected(retrieveValue)
		cell.retrieveSlot.Refresh()
		cell.retrieveSelect.Show()
	}
	if selected {
		cell.background.FillColor = archiveSelectedRowColor
		cell.background.Refresh()
		return
	}
	if disabled {
		cell.background.FillColor = nodeDisabledRowColor
		cell.background.Refresh()
		return
	}
	if tableRow%2 == 0 {
		cell.background.FillColor = archiveEvenRowColor
	} else {
		cell.background.FillColor = archiveOddRowColor
	}
	cell.background.Refresh()
}

func applyNodeHeaderTableCell(cell *nodeTableCell, tableCol int, text string, state *uiState) {
	applyNodeTableCell(cell, 0, tableCol, text, true, false, false, false, false, false, "")
	if cell == nil {
		return
	}
	glyph := nodeHeaderSortGlyph(state, tableCol)
	if glyph == "" {
		return
	}
	cell.sortLabel.SetText(glyph)
	cell.sortLabel.Show()
	cell.sortLabel.Refresh()
}

func nodeTableHeaders() []string {
	return []string{"⊙", "Address", "AETitle", "Port", "Q...", "Retrieve", "Send", "TLS", "Name", "Send Transfer Syntax"}
}

func nodeTableColumnWidths() []float32 {
	return []float32{56, 188, 184, 72, 58, 104, 58, 74, 242, 332}
}

func nodeCheckCell(enabled bool) string {
	if enabled {
		return "☑"
	}
	return "☐"
}

func nodeMenuCell(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		value = "Auto"
	}
	return "▾ " + value
}

func nodeCell(node nodes.Node, col int) string {
	switch col {
	case nodeTableColumnEnabled:
		return nodeCheckCell(node.Enabled())
	case nodeTableColumnQuery:
		return nodeCheckCell(node.QueryEnabled())
	case nodeTableColumnRetrieve:
		return nodeMenuCell(node.RetrieveMethodOrDefault())
	case nodeTableColumnSend:
		return nodeCheckCell(node.SendEnabled())
	case nodeTableColumnTLS:
		if node.UseTLS {
			return nodeMenuCell("Yes")
		}
		return nodeMenuCell("No")
	case nodeTableColumnName:
		return node.Name
	case nodeTableColumnAETitle:
		return node.AETitle
	case nodeTableColumnHost:
		return node.Host
	case nodeTableColumnPort:
		return fmt.Sprintf("%d", node.Port)
	case nodeTableColumnMoveDestination:
		return node.PreferredMoveDestination
	case nodeTableColumnSendSyntax:
		return "▾ " + sendSyntaxTableLabel(node.SendTransferSyntaxOrDefault())
	case nodeTableColumnNotes:
		return node.Notes
	default:
		return ""
	}
}

func queryCell(match query.Match, col int) string {
	switch col {
	case queryRetrieveColumn:
		return ""
	case queryTableColumnLocalState:
		return queryRowLocalStateText(match.LocalState)
	case queryTableColumnLevel:
		return queryLevelCell(match.QueryRetrieveLevel)
	case queryTableColumnPatient:
		return displayPatientName(match.PatientName)
	case queryTableColumnPatientID:
		return match.PatientID
	case queryTableColumnDOB:
		return compactDisplayDate(match.PatientBirthDate)
	case queryTableColumnStudyDate:
		return compactDisplayDate(match.StudyDate)
	case queryTableColumnTime:
		return dicomTimeCell(match.StudyTime)
	case queryTableColumnModality:
		if match.Modality != "" {
			return match.Modality
		}
		return match.Modalities
	case queryTableColumnImages:
		return workstationCountCell(match.ImageCount)
	case queryTableColumnDescription:
		if match.SeriesDescription != "" {
			return match.SeriesDescription
		}
		return match.StudyDescription
	case queryTableColumnAccession:
		return match.AccessionNumber
	case queryTableColumnReferrer:
		return match.ReferringPhysicianName
	case queryTableColumnInstitution:
		return match.InstitutionName
	case queryTableColumnLocalComments:
		return match.LocalComments
	case queryTableColumnServerComments:
		return match.PatientComments
	case queryTableColumnStudyStatus:
		return match.StudyStatusID
	case queryTableColumnSeriesNumber:
		return match.SeriesNumber
	case queryTableColumnInstanceNumber:
		return match.InstanceNumber
	case queryTableColumnStudyUID:
		return match.StudyInstanceUID
	case queryTableColumnSeriesUID:
		return match.SeriesInstanceUID
	case queryTableColumnSOPClass:
		return match.SOPClassUID
	case queryTableColumnSOPUID:
		return match.SOPInstanceUID
	case queryTableColumnSource:
		return querySourceCell(match)
	case queryTableColumnStatus:
		return queryStatusCell(match.Status)
	default:
		return ""
	}
}

func queryLevelCell(level string) string {
	level = strings.ToUpper(strings.TrimSpace(level))
	switch level {
	case "PATIENT":
		return "▾ PATIENT"
	case "STUDY":
		return "  ▸ STUDY"
	case "SERIES":
		return "    ▸ SERIES"
	case "IMAGE":
		return "      IMAGE"
	default:
		return emptyDash(level)
	}
}

func querySourceCell(match query.Match) string {
	name := strings.TrimSpace(match.SourceNodeName)
	host := strings.TrimSpace(match.SourceHost)
	if host == "" || match.SourcePort == 0 {
		return name
	}
	endpoint := fmt.Sprintf("%s:%d", host, match.SourcePort)
	if name == "" {
		return endpoint
	}
	return fmt.Sprintf("%s / %s", name, endpoint)
}

func queryStatusCell(status uint16) string {
	return fmt.Sprintf("0x%04X", status)
}

func queryStatusDotColor(status uint16) color.NRGBA {
	switch status {
	case 0x0000:
		return queryStatusOKColor
	case 0xFF00, 0xFF01:
		return queryStatusPendingColor
	default:
		return queryStatusFailColor
	}
}

func studyStatusCell(status string) string {
	status = strings.TrimSpace(status)
	if status == "" {
		return ""
	}
	switch {
	case strings.EqualFold(status, studyStatusPresetReviewedLabel):
		return "✓ " + studyStatusPresetReviewedLabel
	case strings.EqualFold(status, studyStatusPresetInterestingLabel):
		return "★ " + studyStatusPresetInterestingLabel
	case strings.EqualFold(status, studyStatusPresetFollowUpLabel):
		return "↪ " + studyStatusPresetFollowUpLabel
	case strings.EqualFold(status, studyStatusPresetTeachingLabel):
		return "▣ " + studyStatusPresetTeachingLabel
	case strings.EqualFold(status, studyStatusPresetProblemLabel):
		return "⚠ " + studyStatusPresetProblemLabel
	default:
		return "• " + status
	}
}

func studyStatusDotColor(status string) (color.NRGBA, bool) {
	status = strings.TrimSpace(status)
	if status == "" {
		return color.NRGBA{}, false
	}
	normalized := strings.ToLower(status)
	switch {
	case strings.Contains(normalized, strings.ToLower(studyStatusPresetReviewedLabel)):
		return studyStatusReviewedColor, true
	case strings.Contains(normalized, strings.ToLower(studyStatusPresetInterestingLabel)):
		return studyStatusInterestingColor, true
	case strings.Contains(normalized, strings.ToLower(studyStatusPresetFollowUpLabel)):
		return studyStatusFollowUpColor, true
	case strings.Contains(normalized, strings.ToLower(studyStatusPresetTeachingLabel)):
		return studyStatusTeachingColor, true
	case strings.Contains(normalized, strings.ToLower(studyStatusPresetProblemLabel)):
		return studyStatusProblemColor, true
	default:
		return studyStatusCustomColor, true
	}
}

func studyStatusChipColor(status string) (color.NRGBA, bool) {
	status = strings.TrimSpace(status)
	if status == "" {
		return color.NRGBA{}, false
	}
	normalized := strings.ToLower(status)
	switch {
	case strings.Contains(normalized, strings.ToLower(studyStatusPresetReviewedLabel)):
		return studyStatusReviewedChipColor, true
	case strings.Contains(normalized, strings.ToLower(studyStatusPresetInterestingLabel)):
		return studyStatusInterestingChipColor, true
	case strings.Contains(normalized, strings.ToLower(studyStatusPresetFollowUpLabel)):
		return studyStatusFollowUpChipColor, true
	case strings.Contains(normalized, strings.ToLower(studyStatusPresetTeachingLabel)):
		return studyStatusTeachingChipColor, true
	case strings.Contains(normalized, strings.ToLower(studyStatusPresetProblemLabel)):
		return studyStatusProblemChipColor, true
	default:
		return studyStatusCustomChipColor, true
	}
}

func studyCell(study archive.Study, col int) string {
	switch col {
	case archiveStudyTableColumnPatient:
		return displayPatientName(study.PatientName)
	case archiveStudyTableColumnPatientID:
		return study.PatientID
	case archiveStudyTableColumnDOB:
		return compactDisplayDate(study.PatientBirthDate)
	case archiveStudyTableColumnStudyDate:
		return archiveDateTimeCell(study.StudyDate, study.StudyTime)
	case archiveStudyTableColumnTime:
		return dicomTimeCell(study.StudyTime)
	case archiveStudyTableColumnAdded:
		return archiveTimestampCell(study.ImportedAt)
	case archiveStudyTableColumnModality:
		return study.Modalities
	case archiveStudyTableColumnDescription:
		return study.StudyDescription
	case archiveStudyTableColumnAccession:
		return study.AccessionNumber
	case archiveStudyTableColumnInstitution:
		return study.InstitutionName
	case archiveStudyTableColumnStatus:
		return studyStatusCell(study.Status)
	case archiveStudyTableColumnComments:
		return study.Comments
	case archiveStudyTableColumnSeries:
		return workstationCountCell(strconv.Itoa(study.SeriesCount))
	case archiveStudyTableColumnInstances:
		return workstationCountCell(strconv.Itoa(study.InstanceCount))
	case archiveStudyTableColumnStudyUID:
		return study.StudyInstanceUID
	default:
		return ""
	}
}

func dicomTimeCell(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return ""
	}
	value = strings.SplitN(value, ".", 2)[0]
	if len(value) >= 6 {
		return value[0:2] + ":" + value[2:4] + ":" + value[4:6]
	}
	if len(value) >= 4 {
		return value[0:2] + ":" + value[2:4]
	}
	return value
}

func archiveDateTimeCell(date string, dicomTime string) string {
	return compactDisplayDateTime(date, dicomTime)
}

func archiveTimestampCell(value time.Time) string {
	if value.IsZero() {
		return ""
	}
	return value.Format("2/1/06 15:04")
}

func seriesCell(series archive.Series, col int) string {
	switch col {
	case 0:
		return series.SeriesNumber
	case 1:
		return series.Modality
	case 2:
		return series.SeriesDescription
	case 3:
		return fmt.Sprintf("%d", series.InstanceCount)
	case 4:
		return series.SeriesInstanceUID
	default:
		return ""
	}
}

func instanceCell(instance archive.Instance, col int) string {
	switch col {
	case 0:
		return instance.InstanceNumber
	case 1:
		return instance.Modality
	case 2:
		return instance.SOPClassUID
	case 3:
		if instance.TransferSyntax != "" {
			return instance.TransferSyntax
		}
		return instance.TransferSyntaxUID
	case 4:
		return instance.SourcePath
	case 5:
		return instance.SOPInstanceUID
	default:
		return ""
	}
}

func tableCell(elem dicominspect.ElementSummary, col int) string {
	switch col {
	case 0:
		return elem.Source
	case 1:
		return elem.Tag
	case 2:
		return elem.VR
	case 3:
		if elem.Keyword != "" {
			return elem.Keyword
		}
		return elem.Name
	case 4:
		return elem.Length
	case 5:
		return elem.Value
	default:
		return ""
	}
}

func defaultArchiveDir() string {
	return core.DefaultArchiveDir()
}

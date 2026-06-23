//go:build cgo

package main

/*
#cgo darwin CFLAGS: -x objective-c
#cgo darwin LDFLAGS: -framework Cocoa
#import <Cocoa/Cocoa.h>
#include <stdlib.h>
// Returns newline-joined absolute paths (caller frees), or NULL if cancelled. Runs on main thread.
static char* gopacsOpenPanel(void) {
  __block char* out = NULL;
  void (^work)(void) = ^{
    NSOpenPanel* p = [NSOpenPanel openPanel];
    [p setCanChooseFiles:YES];
    [p setCanChooseDirectories:YES];
    [p setAllowsMultipleSelection:YES];
    [p setResolvesAliases:YES];
    if ([p runModal] == NSModalResponseOK) {
      NSMutableArray* a = [NSMutableArray array];
      for (NSURL* u in [p URLs]) { [a addObject:[u path]]; }
      out = strdup([[a componentsJoinedByString:@"\n"] UTF8String]);
    }
  };
  if ([NSThread isMainThread]) work(); else dispatch_sync(dispatch_get_main_queue(), work);
  return out;
}
// Returns chosen save path (caller frees) or NULL if cancelled.
static char* gopacsSavePanel(const char* def) {
  __block char* out = NULL;
  NSString* name = def ? [NSString stringWithUTF8String:def] : @"export.zip";
  void (^work)(void) = ^{
    NSSavePanel* p = [NSSavePanel savePanel];
    [p setNameFieldStringValue:name];
    if ([p runModal] == NSModalResponseOK) { out = strdup([[[p URL] path] UTF8String]); }
  };
  if ([NSThread isMainThread]) work(); else dispatch_sync(dispatch_get_main_queue(), work);
  return out;
}
// Sets the window to the screen's visible frame (maximize).
static void gopacsMaximize(void* win) {
  if (!win) return;
  void (^work)(void) = ^{ NSWindow* w = (NSWindow*)win; [w setFrame:[[NSScreen mainScreen] visibleFrame] display:YES]; };
  if ([NSThread isMainThread]) work(); else dispatch_sync(dispatch_get_main_queue(), work);
}
*/
import "C"

import (
	"strings"
	"unsafe"

	webview "github.com/webview/webview_go"
)

const windowTitle = "Go PACS"

func runUI(url string, serverDone <-chan error) {
	w := webview.New(false)
	defer w.Destroy()
	w.SetTitle(windowTitle)
	w.SetSize(1280, 800, webview.HintNone)
	// Fill the screen on open. w.Window() returns an unsafe.Pointer to the
	// underlying NSWindow; guard for nil before handing it to Cocoa.
	if win := w.Window(); win != nil {
		C.gopacsMaximize(win)
	}
	// The webview has no native app menu, so Cmd-Q (and Ctrl-Q) would otherwise
	// do nothing. Bind a Go callback and inject a keydown listener that quits.
	_ = w.Bind("__gopacsQuit", func() { w.Terminate() })
	w.Init("document.addEventListener('keydown',function(e){if((e.metaKey||e.ctrlKey)&&(e.key==='q'||e.key==='Q')){e.preventDefault();if(window.__gopacsQuit)window.__gopacsQuit();}});")
	// Native macOS file dialogs bridged into JS. The webview implements neither
	// the HTML file picker nor browser downloads, so Import/Export rely on these.
	_ = w.Bind("__gopacsPickPaths", func() []string {
		c := C.gopacsOpenPanel()
		if c == nil {
			return []string{}
		}
		defer C.free(unsafe.Pointer(c))
		s := C.GoString(c)
		if s == "" {
			return []string{}
		}
		return strings.Split(s, "\n")
	})
	_ = w.Bind("__gopacsPickSave", func(def string) string {
		cn := C.CString(def)
		defer C.free(unsafe.Pointer(cn))
		c := C.gopacsSavePanel(cn)
		if c == nil {
			return ""
		}
		defer C.free(unsafe.Pointer(c))
		return C.GoString(c)
	})
	w.Navigate(url)
	w.Run()
	_ = serverDone
}

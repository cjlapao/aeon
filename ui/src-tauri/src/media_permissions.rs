//! The microphone, for Orion.
//!
//! On Linux the webview (WebKitGTK) refuses `getUserMedia` unless the host switches media streams on and answers its
//! permission request. This does both, and only for an AUDIO-ONLY request: the page may open the microphone, never a
//! camera. The page is Aeon's own bundled UI (its CSP allows nothing else), and Orion still opens the microphone only
//! after the person enables him in Settings.
//!
//! macOS and Windows need nothing here: macOS asks the person through the system prompt (the usage text is in
//! `Info.plist`), and Windows' webview shows its own prompt.

#[cfg(target_os = "linux")]
pub fn allow_audio_capture(window: &tauri::WebviewWindow) {
    use webkit2gtk::{
        glib::prelude::*, PermissionRequestExt, SettingsExt, UserMediaPermissionRequest,
        UserMediaPermissionRequestExt, WebViewExt,
    };
    let _ = window.with_webview(|webview| {
        let wv = webview.inner();
        if let Some(settings) = WebViewExt::settings(&wv) {
            settings.set_enable_media_stream(true);
        }
        wv.connect_permission_request(|_, request| {
            match request.downcast_ref::<UserMediaPermissionRequest>() {
                Some(media) if media.is_for_audio_device() && !media.is_for_video_device() => {
                    request.allow();
                    true
                }
                Some(_) => {
                    request.deny();
                    true
                }
                None => false,
            }
        });
    });
}

#[cfg(not(target_os = "linux"))]
pub fn allow_audio_capture(_window: &tauri::WebviewWindow) {}

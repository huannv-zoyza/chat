#[cfg_attr(mobile, tauri::mobile_entry_point)]
pub fn run() {
  tauri::Builder::default()
    .plugin(tauri_plugin_opener::init())
    .plugin(tauri_plugin_notification::init())
    .setup(|app| {
      if cfg!(debug_assertions) {
        app.handle().plugin(
          tauri_plugin_log::Builder::default()
            .level(log::LevelFilter::Info)
            .build(),
        )?;
      }
      #[cfg(target_os = "linux")]
      enable_media(app)?;
      Ok(())
    })
    .run(tauri::generate_context!())
    .expect("error while building tauri application");
}

/// WebKitGTK mặc định tắt getUserMedia/WebRTC và từ chối mọi yêu cầu quyền.
/// Bật lên để gọi thoại/video được; chỉ tự cho phép yêu cầu micro/camera
/// (các quyền khác như vị trí vẫn bị từ chối như cũ).
#[cfg(target_os = "linux")]
fn enable_media(app: &mut tauri::App) -> Result<(), Box<dyn std::error::Error>> {
  use tauri::Manager;
  use webkit2gtk::glib::prelude::Cast;
  use webkit2gtk::{PermissionRequestExt, SettingsExt, UserMediaPermissionRequest, WebViewExt};

  if let Some(win) = app.get_webview_window("main") {
    win.with_webview(|wv| {
      let view = wv.inner();
      if let Some(settings) = WebViewExt::settings(&view) {
        settings.set_enable_media_stream(true);
        settings.set_enable_webrtc(true);
      }
      view.connect_permission_request(|_, req| {
        if let Some(media) = req.downcast_ref::<UserMediaPermissionRequest>() {
          media.allow();
          return true;
        }
        false
      });
    })?;
  }
  Ok(())
}

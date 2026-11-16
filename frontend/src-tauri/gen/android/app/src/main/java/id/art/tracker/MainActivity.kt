package id.art.tracker

import android.graphics.Color
import android.os.Bundle
import android.view.View
import android.webkit.JavascriptInterface
import android.webkit.WebView
import androidx.activity.enableEdgeToEdge
import androidx.core.view.ViewCompat
import androidx.core.view.WindowCompat
import androidx.core.view.WindowInsetsCompat

// Edge-to-edge is kept (Android 15+ enforces it anyway), but the web content must not
// draw under the status bar, navigation bar, cutout or keyboard: the content root is
// padded by the system insets, and the strips behind them follow the app's own theme
// (which can differ from the system theme) through the ArtrackerNative bridge.
class MainActivity : TauriActivity() {
  private val lightBg = Color.parseColor("#FAFBFD")
  private val darkBg = Color.parseColor("#141519")

  override fun onCreate(savedInstanceState: Bundle?) {
    enableEdgeToEdge()
    super.onCreate(savedInstanceState)

    val content = findViewById<View>(android.R.id.content)
    ViewCompat.setOnApplyWindowInsetsListener(content) { view, insets ->
      val bars = insets.getInsets(
        WindowInsetsCompat.Type.systemBars() or
          WindowInsetsCompat.Type.displayCutout() or
          WindowInsetsCompat.Type.ime()
      )
      view.setPadding(bars.left, bars.top, bars.right, bars.bottom)
      WindowInsetsCompat.CONSUMED
    }
  }

  override fun onWebViewCreate(webView: WebView) {
    webView.addJavascriptInterface(ThemeBridge(), "ArtrackerNative")
  }

  private inner class ThemeBridge {
    @JavascriptInterface
    fun setTheme(theme: String) {
      val dark = theme == "dark"
      runOnUiThread {
        val content = findViewById<View>(android.R.id.content)
        content.setBackgroundColor(if (dark) darkBg else lightBg)
        val controller = WindowCompat.getInsetsController(window, content)
        controller.isAppearanceLightStatusBars = !dark
        controller.isAppearanceLightNavigationBars = !dark
      }
    }
  }
}

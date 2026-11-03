package com.jonbesga.builddrop

import android.app.Application
import android.content.Context
import androidx.lifecycle.AndroidViewModel
import androidx.lifecycle.viewModelScope
import kotlinx.coroutines.CancellationException
import kotlinx.coroutines.Job
import kotlinx.coroutines.delay
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.flow.update
import kotlinx.coroutines.isActive
import kotlinx.coroutines.launch
import java.io.File

sealed interface InstallState {
    data class Downloading(val progress: Float) : InstallState // -1 = indeterminate
    data object Installing : InstallState
    data object Installed : InstallState
    data class Failed(val message: String) : InstallState
}

data class UiState(
    val serverUrl: String,
    val builds: List<Build> = emptyList(),
    val loading: Boolean = false,
    val error: String? = null,
    val installs: Map<String, InstallState> = emptyMap(),
    val needsInstallPermission: Boolean = false,
)

class MainViewModel(app: Application) : AndroidViewModel(app) {
    private val prefs = app.getSharedPreferences("settings", Context.MODE_PRIVATE)
    private val _state = MutableStateFlow(UiState(serverUrl = prefs.getString(KEY_URL, null) ?: BuildConfig.DEFAULT_SERVER_URL))
    val state: StateFlow<UiState> = _state

    private val downloads = mutableMapOf<String, Job>()
    private val apkDir get() = File(getApplication<Application>().cacheDir, "apks").apply { mkdirs() }

    init {
        refresh()
        // Poll while builds are queued or running so status updates live.
        viewModelScope.launch {
            while (isActive) {
                delay(5_000)
                if (_state.value.builds.any { it.isInProgress }) refresh(silent = true)
            }
        }
        viewModelScope.launch {
            ApkInstaller.results.collect { result ->
                val newState = when (result) {
                    is InstallResult.Success -> InstallState.Installed
                    is InstallResult.Failure -> InstallState.Failed(result.message)
                }
                setInstall(result.buildId, newState)
                File(apkDir, "${result.buildId}.apk").delete()
            }
        }
    }

    fun refresh(silent: Boolean = false) {
        viewModelScope.launch {
            if (!silent) _state.update { it.copy(loading = true) }
            try {
                val server = _state.value.serverUrl
                val builds = BuildServerApi(server).listBuilds()
                if (server != _state.value.serverUrl) return@launch
                BuildNotifications.record(getApplication(), server, builds)
                _state.update { it.copy(builds = builds, loading = false, error = null) }
            } catch (e: CancellationException) {
                throw e
            } catch (e: Exception) {
                _state.update { it.copy(loading = false, error = e.message ?: e.toString()) }
            }
        }
    }

    fun setServerUrl(url: String) {
        val clean = url.trim().trimEnd('/')
        prefs.edit().putString(KEY_URL, clean).apply()
        _state.update { it.copy(serverUrl = clean, builds = emptyList(), error = null) }
        refresh()
    }

    fun resetServerUrl() = setServerUrl(BuildConfig.DEFAULT_SERVER_URL)

    fun checkInstallPermission() {
        _state.update { it.copy(needsInstallPermission = !ApkInstaller.canInstall(getApplication())) }
    }

    fun install(build: Build) {
        val context = getApplication<Application>()
        if (!ApkInstaller.canInstall(context)) {
            _state.update { it.copy(needsInstallPermission = true) }
            return
        }
        if (downloads[build.id]?.isActive == true) return
        downloads[build.id] = viewModelScope.launch {
            val apk = File(apkDir, "${build.id}.apk")
            try {
                setInstall(build.id, InstallState.Downloading(0f))
                BuildServerApi(_state.value.serverUrl).downloadApk(build, apk) { p ->
                    setInstall(build.id, InstallState.Downloading(p))
                }
                setInstall(build.id, InstallState.Installing)
                ApkInstaller.install(context, apk, build.id)
            } catch (e: CancellationException) {
                apk.delete()
                clearInstall(build.id)
                throw e
            } catch (e: Exception) {
                apk.delete()
                setInstall(build.id, InstallState.Failed(e.message ?: e.toString()))
            }
        }
    }

    fun cancelDownload(build: Build) {
        downloads.remove(build.id)?.cancel()
    }

    fun clearInstall(buildId: String) = _state.update { it.copy(installs = it.installs - buildId) }

    private fun setInstall(buildId: String, s: InstallState) =
        _state.update { it.copy(installs = it.installs + (buildId to s)) }

    companion object {
        private const val KEY_URL = "server_url"
    }
}

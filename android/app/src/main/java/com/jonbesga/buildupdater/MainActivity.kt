package com.jonbesga.buildupdater

import android.content.Intent
import android.net.Uri
import android.os.Bundle
import android.provider.Settings
import android.text.format.DateUtils
import android.text.format.Formatter
import androidx.activity.ComponentActivity
import androidx.activity.compose.BackHandler
import androidx.activity.compose.setContent
import androidx.activity.enableEdgeToEdge
import androidx.compose.foundation.isSystemInDarkTheme
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Box
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.PaddingValues
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.Spacer
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.width
import androidx.compose.foundation.lazy.LazyColumn
import androidx.compose.foundation.lazy.items
import androidx.compose.foundation.text.KeyboardOptions
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.automirrored.filled.ArrowBack
import androidx.compose.material.icons.filled.Refresh
import androidx.compose.material.icons.filled.Settings
import androidx.compose.material3.AlertDialog
import androidx.compose.material3.Button
import androidx.compose.material3.Card
import androidx.compose.material3.CircularProgressIndicator
import androidx.compose.material3.ExperimentalMaterial3Api
import androidx.compose.material3.FilledTonalButton
import androidx.compose.material3.Icon
import androidx.compose.material3.IconButton
import androidx.compose.material3.LinearProgressIndicator
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.OutlinedButton
import androidx.compose.material3.OutlinedTextField
import androidx.compose.material3.Scaffold
import androidx.compose.material3.Surface
import androidx.compose.material3.Text
import androidx.compose.material3.TextButton
import androidx.compose.material3.TopAppBar
import androidx.compose.material3.darkColorScheme
import androidx.compose.material3.lightColorScheme
import androidx.compose.material3.pulltorefresh.PullToRefreshBox
import androidx.compose.runtime.Composable
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.saveable.rememberSaveable
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.platform.LocalContext
import androidx.compose.ui.text.font.FontFamily
import androidx.compose.ui.text.font.FontWeight
import androidx.compose.ui.text.input.KeyboardType
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.ui.unit.dp
import androidx.lifecycle.compose.LifecycleResumeEffect
import androidx.lifecycle.compose.collectAsStateWithLifecycle
import androidx.lifecycle.viewmodel.compose.viewModel

class MainActivity : ComponentActivity() {
    override fun onCreate(savedInstanceState: Bundle?) {
        super.onCreate(savedInstanceState)
        enableEdgeToEdge()
        setContent {
            MaterialTheme(colorScheme = if (isSystemInDarkTheme()) darkColorScheme() else lightColorScheme()) {
                Surface(Modifier.fillMaxSize()) { App() }
            }
        }
    }
}

@Composable
fun App(vm: MainViewModel = viewModel()) {
    val state by vm.state.collectAsStateWithLifecycle()
    var showSettings by rememberSaveable { mutableStateOf(false) }

    // Re-check the "install unknown apps" permission when returning from system settings.
    LifecycleResumeEffect(Unit) {
        vm.checkInstallPermission()
        onPauseOrDispose {}
    }

    if (showSettings) {
        BackHandler { showSettings = false }
        SettingsScreen(
            currentUrl = state.serverUrl,
            onSave = { vm.setServerUrl(it); showSettings = false },
            onReset = { vm.resetServerUrl(); showSettings = false },
            onBack = { showSettings = false },
        )
    } else {
        BuildsScreen(state, vm, onOpenSettings = { showSettings = true })
    }
}

@OptIn(ExperimentalMaterial3Api::class)
@Composable
fun BuildsScreen(state: UiState, vm: MainViewModel, onOpenSettings: () -> Unit) {
    val context = LocalContext.current
    var permissionDialog by remember { mutableStateOf(false) }
    if (state.needsInstallPermission && permissionDialog) {
        AlertDialog(
            onDismissRequest = { permissionDialog = false },
            title = { Text("Allow installs") },
            text = { Text("To install builds, allow Build Updater to install unknown apps.") },
            confirmButton = {
                TextButton(onClick = {
                    permissionDialog = false
                    context.startActivity(
                        Intent(Settings.ACTION_MANAGE_UNKNOWN_APP_SOURCES, Uri.parse("package:${context.packageName}"))
                    )
                }) { Text("Open settings") }
            },
            dismissButton = { TextButton(onClick = { permissionDialog = false }) { Text("Cancel") } },
        )
    }

    Scaffold(
        topBar = {
            TopAppBar(
                title = {
                    Column {
                        Text("Builds")
                        Text(
                            state.serverUrl,
                            style = MaterialTheme.typography.labelSmall,
                            color = MaterialTheme.colorScheme.onSurfaceVariant,
                        )
                    }
                },
                actions = {
                    IconButton(onClick = { vm.refresh() }) { Icon(Icons.Default.Refresh, "Refresh") }
                    IconButton(onClick = onOpenSettings) { Icon(Icons.Default.Settings, "Settings") }
                },
            )
        },
    ) { padding ->
        PullToRefreshBox(
            isRefreshing = state.loading,
            onRefresh = { vm.refresh() },
            modifier = Modifier.padding(padding).fillMaxSize(),
        ) {
            LazyColumn(
                contentPadding = PaddingValues(16.dp),
                verticalArrangement = Arrangement.spacedBy(12.dp),
                modifier = Modifier.fillMaxSize(),
            ) {
                state.error?.let { err ->
                    item {
                        Card {
                            Column(Modifier.padding(16.dp)) {
                                Text("Can't reach server", fontWeight = FontWeight.Bold, color = MaterialTheme.colorScheme.error)
                                Text(err, style = MaterialTheme.typography.bodySmall)
                                Spacer(Modifier.padding(4.dp))
                                OutlinedButton(onClick = onOpenSettings) { Text("Check server settings") }
                            }
                        }
                    }
                }
                if (state.builds.isEmpty() && state.error == null && !state.loading) {
                    item {
                        Text(
                            "No builds yet. Ask Claude to build an Android project with the android-build skill.",
                            color = MaterialTheme.colorScheme.onSurfaceVariant,
                            modifier = Modifier.padding(16.dp),
                        )
                    }
                }
                items(state.builds, key = { it.id }) { build ->
                    BuildCard(
                        build = build,
                        install = state.installs[build.id],
                        onInstall = {
                            if (state.needsInstallPermission) permissionDialog = true else vm.install(build)
                        },
                        onCancel = { vm.cancelDownload(build) },
                        onDismiss = { vm.clearInstall(build.id) },
                    )
                }
            }
        }
    }
}

@Composable
fun BuildCard(
    build: Build,
    install: InstallState?,
    onInstall: () -> Unit,
    onCancel: () -> Unit,
    onDismiss: () -> Unit,
) {
    val context = LocalContext.current
    Card(Modifier.fillMaxWidth()) {
        Column(Modifier.padding(16.dp)) {
            Row(verticalAlignment = Alignment.CenterVertically) {
                Column(Modifier.weight(1f)) {
                    Text(build.project, style = MaterialTheme.typography.titleMedium, fontWeight = FontWeight.SemiBold)
                    if (build.versionName.isNotEmpty()) {
                        Text(
                            "v${build.versionName} (${build.versionCode})",
                            style = MaterialTheme.typography.bodySmall,
                        )
                    }
                }
                StatusChip(build.status)
            }
            if (build.packageName.isNotEmpty()) {
                Text(build.packageName, style = MaterialTheme.typography.bodySmall, color = MaterialTheme.colorScheme.onSurfaceVariant)
            }
            if (build.commit.isNotEmpty()) {
                Spacer(Modifier.padding(2.dp))
                Text(
                    "${build.branch} @ ${build.commit}",
                    style = MaterialTheme.typography.bodySmall,
                    fontFamily = FontFamily.Monospace,
                )
                if (build.commitMessage.isNotEmpty()) {
                    Text(build.commitMessage, style = MaterialTheme.typography.bodySmall, maxLines = 2, overflow = TextOverflow.Ellipsis)
                }
            }
            val meta = buildList {
                build.createdAt?.let {
                    add(DateUtils.getRelativeTimeSpanString(it.toEpochMilli()).toString())
                }
                // The default task says nothing; only show non-default ones (e.g. assembleRelease).
                if (build.task != "assembleDebug") add(build.task)
                if (build.apkSize > 0) add(Formatter.formatShortFileSize(context, build.apkSize))
            }
            Text(meta.joinToString(" · "), style = MaterialTheme.typography.labelSmall, color = MaterialTheme.colorScheme.onSurfaceVariant)
            if (build.status == "failed" && build.error.isNotEmpty()) {
                Text(build.error, style = MaterialTheme.typography.bodySmall, color = MaterialTheme.colorScheme.error)
            }

            if (build.isInstallable) {
                Spacer(Modifier.padding(4.dp))
                InstallControls(install, onInstall, onCancel, onDismiss)
            }
        }
    }
}

@Composable
fun InstallControls(install: InstallState?, onInstall: () -> Unit, onCancel: () -> Unit, onDismiss: () -> Unit) {
    when (install) {
        null -> Button(onClick = onInstall, modifier = Modifier.fillMaxWidth()) { Text("Download & install") }
        is InstallState.Downloading -> Column {
            Row(verticalAlignment = Alignment.CenterVertically) {
                Text(
                    if (install.progress >= 0) "Downloading… ${(install.progress * 100).toInt()}%" else "Downloading…",
                    style = MaterialTheme.typography.bodySmall,
                    modifier = Modifier.weight(1f),
                )
                TextButton(onClick = onCancel) { Text("Cancel") }
            }
            if (install.progress >= 0) {
                LinearProgressIndicator(progress = { install.progress }, modifier = Modifier.fillMaxWidth())
            } else {
                LinearProgressIndicator(Modifier.fillMaxWidth())
            }
        }
        InstallState.Installing -> Row(verticalAlignment = Alignment.CenterVertically) {
            CircularProgressIndicator(Modifier.padding(end = 12.dp).width(20.dp), strokeWidth = 2.dp)
            Text("Installing…", style = MaterialTheme.typography.bodySmall)
        }
        InstallState.Installed -> Row(verticalAlignment = Alignment.CenterVertically) {
            Text("Installed ✓", color = Color(0xFF2E7D32), modifier = Modifier.weight(1f))
            TextButton(onClick = onInstall) { Text("Reinstall") }
        }
        is InstallState.Failed -> Column {
            Text(install.message, color = MaterialTheme.colorScheme.error, style = MaterialTheme.typography.bodySmall)
            Row {
                FilledTonalButton(onClick = onInstall) { Text("Retry") }
                Spacer(Modifier.width(8.dp))
                TextButton(onClick = onDismiss) { Text("Dismiss") }
            }
        }
    }
}

@Composable
fun StatusChip(status: String) {
    val (label, color) = when (status) {
        "success" -> "Ready" to Color(0xFF2E7D32)
        "failed" -> "Failed" to MaterialTheme.colorScheme.error
        "building" -> "Building" to Color(0xFFEF6C00)
        "queued" -> "Queued" to Color(0xFF616161)
        else -> status to Color(0xFF616161)
    }
    Surface(color = color.copy(alpha = 0.15f), shape = MaterialTheme.shapes.small) {
        Row(Modifier.padding(horizontal = 8.dp, vertical = 4.dp), verticalAlignment = Alignment.CenterVertically) {
            if (status == "building" || status == "queued") {
                CircularProgressIndicator(Modifier.width(12.dp).padding(end = 0.dp), strokeWidth = 2.dp, color = color)
                Spacer(Modifier.width(6.dp))
            }
            Text(label, color = color, style = MaterialTheme.typography.labelMedium)
        }
    }
}

@OptIn(ExperimentalMaterial3Api::class)
@Composable
fun SettingsScreen(currentUrl: String, onSave: (String) -> Unit, onReset: () -> Unit, onBack: () -> Unit) {
    var url by rememberSaveable { mutableStateOf(currentUrl) }
    val valid = url.trim().let { it.startsWith("http://") || it.startsWith("https://") }
    Scaffold(
        topBar = {
            TopAppBar(
                title = { Text("Settings") },
                navigationIcon = {
                    IconButton(onClick = onBack) { Icon(Icons.AutoMirrored.Filled.ArrowBack, "Back") }
                },
            )
        },
    ) { padding ->
        Column(Modifier.padding(padding).padding(16.dp), verticalArrangement = Arrangement.spacedBy(12.dp)) {
            OutlinedTextField(
                value = url,
                onValueChange = { url = it },
                label = { Text("Build server URL") },
                singleLine = true,
                isError = !valid,
                supportingText = { Text("Default: ${BuildConfig.DEFAULT_SERVER_URL}") },
                keyboardOptions = KeyboardOptions(keyboardType = KeyboardType.Uri),
                modifier = Modifier.fillMaxWidth(),
            )
            Row {
                Button(onClick = { onSave(url) }, enabled = valid) { Text("Save") }
                Spacer(Modifier.width(8.dp))
                OutlinedButton(onClick = onReset) { Text("Reset to default") }
            }
            Box(Modifier.padding(top = 8.dp)) {
                Text(
                    "App version ${BuildConfig.VERSION_NAME}",
                    style = MaterialTheme.typography.labelSmall,
                    color = MaterialTheme.colorScheme.onSurfaceVariant,
                )
            }
        }
    }
}

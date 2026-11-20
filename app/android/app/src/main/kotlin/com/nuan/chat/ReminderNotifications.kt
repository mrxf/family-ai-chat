package com.nuan.chat

import android.app.AlarmManager
import android.app.Notification
import android.app.NotificationChannel
import android.app.NotificationManager
import android.app.PendingIntent
import android.content.BroadcastReceiver
import android.content.Context
import android.content.Intent
import android.os.Build
import org.json.JSONArray
import org.json.JSONObject

object ReminderScheduler {
    const val CHANNEL_ID = "nuan_schedule_reminders"
    const val EXTRA_OPEN_SCHEDULES = "com.nuan.chat.OPEN_SCHEDULES"
    private const val PREFS_NAME = "nuan_schedule_reminders_prefs"
    private const val KEY_ITEMS = "scheduled_items"
    private const val BASE_REQ_CODE = 24000

    private const val EXTRA_CODE = "code"
    private const val EXTRA_TITLE = "title"
    private const val EXTRA_BODY = "body"

    fun ensureChannel(context: Context) {
        if (Build.VERSION.SDK_INT >= Build.VERSION_CODES.O) {
            val nm = context.getSystemService(Context.NOTIFICATION_SERVICE) as NotificationManager
            val existing = nm.getNotificationChannel(CHANNEL_ID)
            if (existing == null) {
                val ch = NotificationChannel(
                    CHANNEL_ID,
                    "小暖日程提醒",
                    NotificationManager.IMPORTANCE_HIGH
                ).apply {
                    description = "在小暖里设定的吃药、复查与日常待办准点提醒"
                    enableVibration(true)
                }
                nm.createNotificationChannel(ch)
            }
        }
    }

    @Synchronized
    fun sync(context: Context, list: List<Map<*, *>>) {
        val appCtx = context.applicationContext
        ensureChannel(appCtx)
        cancelStoredAlarms(appCtx)

        val now = System.currentTimeMillis()
        val arr = JSONArray()
        var slot = 0

        for (raw in list) {
            if (slot >= 64) break
            val fireAt = (raw["fire_at"] as? Number)?.toLong() ?: continue
            if (fireAt <= now - 5_000L) continue
            val title = (raw["title"] as? String)?.trim()?.takeIf { it.isNotEmpty() } ?: "日程提醒"
            val body = (raw["body"] as? String)?.trim().orEmpty()
            val code = BASE_REQ_CODE + slot
            slot++

            val obj = JSONObject().apply {
                put("code", code)
                put("fire_at", fireAt)
                put("title", title)
                put("body", body)
            }
            arr.put(obj)
            scheduleOne(appCtx, code, fireAt, title, body)
        }

        appCtx.getSharedPreferences(PREFS_NAME, Context.MODE_PRIVATE)
            .edit()
            .putString(KEY_ITEMS, arr.toString())
            .apply()
    }

    @Synchronized
    fun clearAll(context: Context) {
        val appCtx = context.applicationContext
        cancelStoredAlarms(appCtx)
        appCtx.getSharedPreferences(PREFS_NAME, Context.MODE_PRIVATE)
            .edit()
            .remove(KEY_ITEMS)
            .apply()
    }

    @Synchronized
    fun rescheduleStored(context: Context) {
        val appCtx = context.applicationContext
        ensureChannel(appCtx)
        val raw = appCtx.getSharedPreferences(PREFS_NAME, Context.MODE_PRIVATE)
            .getString(KEY_ITEMS, null) ?: return
        val now = System.currentTimeMillis()
        try {
            val arr = JSONArray(raw)
            for (i in 0 until arr.length()) {
                val obj = arr.optJSONObject(i) ?: continue
                val fireAt = obj.optLong("fire_at", 0L)
                if (fireAt <= now) continue
                val code = obj.optInt("code", BASE_REQ_CODE + i)
                val title = obj.optString("title", "日程提醒")
                val body = obj.optString("body", "")
                scheduleOne(appCtx, code, fireAt, title, body)
            }
        } catch (_: Exception) {
        }
    }

    private fun cancelStoredAlarms(context: Context) {
        val am = context.getSystemService(Context.ALARM_SERVICE) as AlarmManager
        val raw = context.getSharedPreferences(PREFS_NAME, Context.MODE_PRIVATE)
            .getString(KEY_ITEMS, null) ?: return
        try {
            val arr = JSONArray(raw)
            for (i in 0 until arr.length()) {
                val code = arr.optJSONObject(i)?.optInt("code", BASE_REQ_CODE + i) ?: (BASE_REQ_CODE + i)
                val intent = Intent(context, ReminderAlarmReceiver::class.java)
                val pi = PendingIntent.getBroadcast(
                    context,
                    code,
                    intent,
                    PendingIntent.FLAG_NO_CREATE or PendingIntent.FLAG_IMMUTABLE
                )
                if (pi != null) {
                    am.cancel(pi)
                    pi.cancel()
                }
            }
        } catch (_: Exception) {
        }
    }

    private fun scheduleOne(context: Context, code: Int, fireAt: Long, title: String, body: String) {
        val am = context.getSystemService(Context.ALARM_SERVICE) as AlarmManager
        val intent = Intent(context, ReminderAlarmReceiver::class.java).apply {
            putExtra(EXTRA_CODE, code)
            putExtra(EXTRA_TITLE, title)
            putExtra(EXTRA_BODY, body)
        }
        val pi = PendingIntent.getBroadcast(
            context,
            code,
            intent,
            PendingIntent.FLAG_UPDATE_CURRENT or PendingIntent.FLAG_IMMUTABLE
        )
        val triggerAt = fireAt.coerceAtLeast(System.currentTimeMillis() + 1_000L)
        try {
            val canExact = if (Build.VERSION.SDK_INT >= Build.VERSION_CODES.S) {
                am.canScheduleExactAlarms()
            } else {
                true
            }
            if (canExact) {
                am.setExactAndAllowWhileIdle(AlarmManager.RTC_WAKEUP, triggerAt, pi)
            } else {
                am.setAndAllowWhileIdle(AlarmManager.RTC_WAKEUP, triggerAt, pi)
            }
        } catch (_: SecurityException) {
            am.setAndAllowWhileIdle(AlarmManager.RTC_WAKEUP, triggerAt, pi)
        } catch (_: Exception) {
        }
    }

    fun showNotification(context: Context, intent: Intent) {
        val appCtx = context.applicationContext
        ensureChannel(appCtx)
        val nm = appCtx.getSystemService(Context.NOTIFICATION_SERVICE) as NotificationManager
        if (!nm.areNotificationsEnabled()) return

        val code = intent.getIntExtra(EXTRA_CODE, BASE_REQ_CODE)
        val title = intent.getStringExtra(EXTRA_TITLE)?.takeIf { it.isNotBlank() } ?: "小暖日程提醒"
        val body = intent.getStringExtra(EXTRA_BODY)?.takeIf { it.isNotBlank() } ?: "您设定的日程时间到啦"

        val tapIntent = Intent(appCtx, MainActivity::class.java).apply {
            flags = Intent.FLAG_ACTIVITY_NEW_TASK or Intent.FLAG_ACTIVITY_SINGLE_TOP or Intent.FLAG_ACTIVITY_CLEAR_TOP
            putExtra(EXTRA_OPEN_SCHEDULES, true)
        }
        val tapPi = PendingIntent.getActivity(
            appCtx,
            code,
            tapIntent,
            PendingIntent.FLAG_UPDATE_CURRENT or PendingIntent.FLAG_IMMUTABLE
        )

        val builder = if (Build.VERSION.SDK_INT >= Build.VERSION_CODES.O) {
            Notification.Builder(appCtx, CHANNEL_ID)
        } else {
            @Suppress("DEPRECATION")
            Notification.Builder(appCtx).setPriority(Notification.PRIORITY_HIGH).setDefaults(Notification.DEFAULT_ALL)
        }

        val notification = builder
            .setSmallIcon(appCtx.applicationInfo.icon)
            .setContentTitle(title)
            .setContentText(body)
            .setStyle(Notification.BigTextStyle().bigText(body))
            .setContentIntent(tapPi)
            .setAutoCancel(true)
            .setCategory(Notification.CATEGORY_REMINDER)
            .setVisibility(Notification.VISIBILITY_PRIVATE)
            .build()

        nm.notify(code, notification)
    }
}

class ReminderAlarmReceiver : BroadcastReceiver() {
    override fun onReceive(context: Context, intent: Intent?) {
        if (intent != null) {
            ReminderScheduler.showNotification(context, intent)
        }
    }
}

class ReminderBootReceiver : BroadcastReceiver() {
    override fun onReceive(context: Context, intent: Intent?) {
        val action = intent?.action ?: return
        if (action == Intent.ACTION_BOOT_COMPLETED || action == Intent.ACTION_MY_PACKAGE_REPLACED) {
            ReminderScheduler.rescheduleStored(context)
        }
    }
}

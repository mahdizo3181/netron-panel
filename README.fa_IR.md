<div dir="rtl">

<p align="center">
  <picture>
    <source media="(prefers-color-scheme: dark)" srcset="./media/netron-logo-dark.svg">
    <img alt="netron panel" src="./media/netron-logo-light.svg" width="360">
  </picture>
</p>

<p align="center">
  <b>فورکی از 3x-ui با ضریب مصرف برای هر اینباند و یک سهمیه‌ی مشترک برای هر کلاینت</b>
</p>

<p align="center">
  <a href="https://github.com/mahdizo3181/netron-panel/releases"><img src="https://img.shields.io/github/v/release/mahdizo3181/netron-panel" alt="Release"></a>
  <a href="https://github.com/mahdizo3181/netron-panel/actions"><img src="https://img.shields.io/github/actions/workflow/status/mahdizo3181/netron-panel/release.yml.svg" alt="Build"></a>
  <a href="https://www.gnu.org/licenses/gpl-3.0.en.html"><img src="https://img.shields.io/badge/license-GPL--3.0-green.svg" alt="License"></a>
</p>

<p align="center">
  <a href="README.md">English</a> · فارسی
</p>

**netron-panel** یک پنل وب برای مدیریت سرورهای [Xray-core](https://github.com/XTLS/Xray-core) است. این پروژه فورکی از [MHSanaei/3x-ui](https://github.com/MHSanaei/3x-ui) (نسخه‌ی پایه `v3.11.0`) است و همه‌ی امکانات 3x-ui (اینباند چندپروتکلی، سهمیه‌ی کلاینت، چندنودی، اشتراک، ربات و API) را حفظ می‌کند و موارد زیر را به آن اضافه می‌کند.

> [!NOTE]
> netron-panel با همان مجوز [GPL-3.0](LICENSE) منتشر می‌شود. تمام اعتبار 3X-UI متعلق به [MHSanaei](https://github.com/MHSanaei) و مشارکت‌کنندگان آن است. فایل‌های upstream که این فورک تغییر داده در [docs/NETRON_UPSTREAM_CHANGES.md](docs/NETRON_UPSTREAM_CHANGES.md) فهرست شده‌اند.

## netron-panel چه چیزی اضافه می‌کند

### ضریب مصرف برای هر اینباند، با یک سهمیه‌ی مشترک

یک کلاینت (یک ایمیل، یک لینک اشتراک) می‌تواند روی چند اینباند باشد و هر اینباند ضریب خودش را دارد؛ مثلاً `direct ×1` و `tunnel ×1.2`. حجم واقعی عبوری از هر اینباند ضرب‌در ضریب همان اینباند به **یک** شمارنده‌ی سهمیه‌ی کلاینت اضافه می‌شود.

> ۱۰ گیگ روی اینباند ×۱ + ۵ گیگ روی اینباند ×۱.۲ = ۱۰ + ۶ = **۱۶ گیگ** مصرف. کلاینتی با سهمیه‌ی ۱۶ گیگ دقیقاً همین‌جا قطع می‌شود.

- **بدون تغییر برای کاربر نهایی.** لینک‌های اشتراک عوض نمی‌شوند و کلاینت تکراری ساخته نمی‌شود. قطع سهمیه، انقضا، ریست، اعداد پنل، گزارش ربات‌ها و هدر `Subscription-Userinfo` همگی عدد ضریب‌دار را نشان می‌دهند.
- **بازه‌ی `0` تا `10` و هر مقدار اعشاری.** پیش‌فرض `1`. مقدار `0` یعنی رایگان: ترافیک ثبت می‌شود ولی از سهمیه کم نمی‌شود.
- **فقط برای ترافیک آینده.** تغییر ضریب هرگز چیزی را که قبلاً حساب شده بازنویسی نمی‌کند.
- **حجم واقعی و حجم محاسبه‌شده** برای هر اینباند جدا ذخیره می‌شود؛ مجموع اینباند همچنان حجم واقعی است.
- **سازگار با نود.** هر نود حسابداری خودش را دارد و مستر فقط جمع‌های ضریب‌دار را می‌گیرد و دوباره ضرب نمی‌کند. اگر نودی قدیمی باشد یا حالت ضریب در آن خاموش باشد، مستر هشدار می‌دهد.
- **پروتکل‌های پشتیبانی‌شده:** VLESS، VMess، Trojan، Shadowsocks (چندکاربره)، Hysteria، MASQUE. اینباندهای WireGuard، TUIC، MTProto، AmneziaWG و tunnel/http/mixed/tun باید روی ×۱ بمانند.

**پیش‌فرض ایمن.** این حالت پشت یک کلید است: **تنظیمات ← فعال‌سازی ضریب هر اینباند**، که به‌صورت پیش‌فرض **خاموش** است. حالت خاموش دقیقاً مثل 3x-ui اصلی رفتار می‌کند. ذخیره‌ی کلید آن را اعمال نمی‌کند؛ با اولین ری‌استارت اجباری Xray فعال می‌شود و در همان لحظه یک‌بار همه‌ی اتصال‌ها قطع می‌شوند. صفحه‌ی تنظیمات وضعیت «ذخیره‌شده» و «در حال اجرا» را نشان می‌دهد.

جزئیات کار، ترتیب به‌روزرسانی در حالت چندنودی، محدودیت‌ها و راهنمای تست: **[docs/MULTIPLIER.md](docs/MULTIPLIER.md)**

### کانفیگ اطلاعات سازگار با Happ

وقتی «کانفیگ اطلاعات / نود ساختگی» روشن است، اشتراک با یک ورودی ساختگی شروع می‌شود که حجم و روز باقی‌مانده را نشان می‌دهد (و برای کلاینت منقضی یا تمام‌شده تنها ورودی است). نسخه‌های جدید Happ اشتراکی که ورودی SOCKS دارد را رد می‌کنند، برای همین netron-panel به‌طور پیش‌فرض یک ورودی VLESS غیرعملیاتی می‌سازد. برگشت به SOCKS با یک تنظیم ممکن است. جزئیات: **[docs/INFO_CONFIG.md](docs/INFO_CONFIG.md)**

### تبدیل درجا از 3x-ui اصلی

روی سروری که 3x-ui اصلی دارد، دستور نصب زیر را بزنید؛ نصب فعلی درجا تبدیل می‌شود: اول بکاپ، بعد بررسی سلامت و در صورت هر خطا **برگشت خودکار**، و بعداً هم با `x-ui rollback` می‌توان برگشت. اسکریپت Docker، PostgreSQL، نصب ناقص و نسخه‌ی خیلی قدیمی را به‌جای حدس زدن رد می‌کند. مهاجرت دیتابیس یک‌طرفه است، پس بکاپ را نگه دارید. جزئیات: **[docs/UPGRADE_FROM_STOCK.md](docs/UPGRADE_FROM_STOCK.md)**

### انتشارها از همین مخزن می‌آیند

`install.sh`، `update.sh`، منوی `x-ui` و دکمه‌ی به‌روزرسانی پنل همگی از `mahdizo3181/netron-panel` دریافت می‌کنند. هر انتشار شامل بسته‌ی linux `amd64` و `arm64` به‌همراه فایل `.sha256` است که نصب‌کننده آن را بررسی می‌کند.

> [!WARNING]
> **هرگز نصب‌کننده یا به‌روزرسان اصلی upstream را روی سرور netron اجرا نکنید.** باینری با 3x-ui اصلی جایگزین می‌شود که ضریب را نادیده می‌گیرد و همه‌ی اینباندها را ×۱ حساب می‌کند.

## نصب سریع

روی Ubuntu/Debian (‏amd64 یا arm64) با کاربر root:

```bash
bash <(curl -Ls https://raw.githubusercontent.com/mahdizo3181/netron-panel/main/install.sh)
```

برای نصب یک نسخه‌ی مشخص، تگ آن را اضافه کنید (مثلاً `v3.11.0-netron.5`):

```bash
bash <(curl -Ls https://raw.githubusercontent.com/mahdizo3181/netron-panel/main/install.sh) v3.11.0-netron.5
```

حین نصب، نام کاربری، رمز عبور و مسیر دسترسی تصادفی ساخته می‌شود. بعد از نصب، دستور `x-ui` منوی مدیریت را باز می‌کند (شروع/توقف سرویس، مشاهده یا ریست اطلاعات ورود، مدیریت SSL و غیره).

برای استفاده‌ی عمومی از پنل (اینباند، کلاینت، مسیریابی، API) مستندات upstream در [docs.sanaei.dev](https://docs.sanaei.dev/fa) همچنان معتبر است، مگر در مواردی که این README یا مستندات netron چیز دیگری بگوید.

## پایگاه داده

- **SQLite** (پیش‌فرض): یک فایل در `/etc/x-ui/x-ui.db`. بدون نیاز به تنظیم.
- **PostgreSQL**: برای تعداد کلاینت بالا یا چندنودی. توجه: تبدیل درجا از 3x-ui اصلی نصب‌های PostgreSQL را پشتیبانی نمی‌کند.

## داکر

netron-panel ایمیج داکر **منتشر نمی‌کند** (`ghcr.io/mhsanaei/3x-ui` همان 3x-ui اصلی است و ضریب را نادیده می‌گیرد). ایمیج این فورک را با `docker compose build` به‌صورت محلی بسازید.

## مستندات

| سند | محتوا |
| --- | --- |
| [docs/MULTIPLIER.md](docs/MULTIPLIER.md) | طراحی ضریب، کلید فعال‌سازی، نودها، ترتیب به‌روزرسانی، محدودیت‌ها، ساخت ریلیز |
| [docs/INFO_CONFIG.md](docs/INFO_CONFIG.md) | ورودی ساختگی اطلاعات در اشتراک (VLESS یا SOCKS) |
| [docs/UPGRADE_FROM_STOCK.md](docs/UPGRADE_FROM_STOCK.md) | تبدیل درجا، بکاپ، برگشت، اجرای آزمایشی |
| [docs/NETRON_UPSTREAM_CHANGES.md](docs/NETRON_UPSTREAM_CHANGES.md) | همه‌ی فایل‌های upstream که این فورک تغییر داده |

برای متغیرهای محیطی، نصب بدون تعامل (cloud-init) و سایر بخش‌ها نسخه‌ی انگلیسی [README.md](README.md) را ببینید.

## قدردانی

- [MHSanaei/3x-ui](https://github.com/MHSanaei/3x-ui) و مشارکت‌کنندگان آن، پروژه‌ای که این فورک بر پایه‌ی آن ساخته شده.
- [alireza0](https://github.com/alireza0/)
- [Iran v2ray rules](https://github.com/chocolate4u/Iran-v2ray-rules) و [Russia v2ray rules](https://github.com/runetfreedom/russia-v2ray-rules-dat) (هر دو GPL-3.0)
- ایده‌ی هویت آماری جداگانه برای هر کلاینت و اینباند از [Heimdall-Panel](https://github.com/HeimdallStudio/Heimdall-Panel) الهام گرفته شده است.

> [!IMPORTANT]
> این پروژه برای استفاده‌ی شخصی است. از آن برای مقاصد غیرقانونی استفاده نکنید.

## مجوز

[GPL-3.0](LICENSE). این پروژه اثر مشتق‌شده از [3x-ui](https://github.com/MHSanaei/3x-ui) نوشته‌ی MHSanaei است.

</div>

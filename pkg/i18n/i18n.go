package i18n

import "strings"

var translations = map[string]string{
	"invalid request":                              "درخواست نامعتبر است",
	"failed to generate token":                     "خطا در تولید توکن",
	"failed to get user":                           "خطا در دریافت کاربر",
	"missing authorization token":                  "توکن احراز هویت ارسال نشده است",
	"invalid token":                                "توکن نامعتبر است",
	"failed to validate user":                      "خطا در اعتبارسنجی کاربر",
	"user not found":                               "کاربر یافت نشد",
	"unauthorized":                                 "دسترسی غیرمجاز",
	"user_id query parameter required":             "پارامتر user_id الزامی است",
	"invalid user_id":                              "user_id نامعتبر است",
	"failed to check conversation":                 "خطا در بررسی مکالمه",
	"conversation not found":                       "مکالمه یافت نشد",
	"failed to fetch messages":                     "خطا در دریافت پیام ها",
	"failed to scan message":                       "خطا در پردازش پیام",
	"failed to fetch conversations":                "خطا در دریافت مکالمه ها",
	"failed to fetch conversation":                 "خطا در دریافت مکالمه",
	"invalid message id":                           "شناسه پیام نامعتبر است",
	"cannot mark this message":                     "امکان تغییر وضعیت این پیام وجود ندارد",
	"failed to update message":                     "خطا در به روزرسانی پیام",
	"message not found":                            "پیام یافت نشد",
	"failed to fetch message":                      "خطا در دریافت پیام",
	"can only delete own messages":                 "فقط پیام های خودتان قابل حذف است",
	"failed to delete message":                     "خطا در حذف پیام",
	"username required":                            "نام کاربری الزامی است",
	"failed to fetch user":                         "خطا در دریافت کاربر",
	"failed to fetch users":                        "خطا در دریافت کاربران",
	"cannot create conversation with yourself":     "نمی توانید با خودتان مکالمه ایجاد کنید",
	"participant not found":                        "کاربر مقابل یافت نشد",
	"failed to create conversation":                "خطا در ایجاد مکالمه",
	"invalid conversation id":                      "شناسه مکالمه نامعتبر است",
	"failed to start transaction":                  "خطا در شروع تراکنش",
	"not a participant":                            "شما عضو این مکالمه نیستید",
	"invalid participants":                         "شرکت کنندگان نامعتبر هستند",
	"failed to fetch files":                        "خطا در دریافت فایل ها",
	"failed to delete files":                       "خطا در حذف فایل ها",
	"failed to delete messages":                    "خطا در حذف پیام ها",
	"failed to delete conversation":                "خطا در حذف مکالمه",
	"failed to commit delete":                      "خطا در نهایی سازی حذف",
	"file is required":                             "فایل الزامی است",
	"file too large":                               "حجم فایل بیش از حد مجاز است",
	"invalid receiver_id":                          "receiver_id نامعتبر است",
	"failed to create message":                     "خطا در ایجاد پیام",
	"failed to save file":                          "خطا در ذخیره فایل",
	"failed to save file record":                   "خطا در ثبت اطلاعات فایل",
	"failed to update profile":                     "خطا در به روزرسانی پروفایل",
	"avatar file is required":                      "فایل آواتار الزامی است",
	"file must be an image":                        "فایل باید تصویر باشد",
	"avatar must be smaller than 500KB":            "حجم آواتار باید کمتر از ۵۰۰ کیلوبایت باشد",
	"failed to save avatar":                        "خطا در ذخیره آواتار",
	"failed to update avatar":                      "خطا در به روزرسانی آواتار",
	"failed to delete conversations":               "خطا در حذف مکالمه ها",
	"failed to delete user":                        "خطا در حذف کاربر",
	"failed to fetch profile":                      "خطا در دریافت پروفایل",
	"websocket upgrade failed":                     "خطا در برقراری اتصال وب سوکت",
	"rate limiter error":                           "خطا در محدودسازی درخواست ها",
	"rate limit exceeded":                          "تعداد درخواست ها بیش از حد مجاز است",
	"internal server error":                        "خطای داخلی سرور",
	"not found":                                    "یافت نشد",
	"username must be between 3 and 32 characters": "نام کاربری باید بین ۳ تا ۳۲ کاراکتر باشد",
	"username can only contain letters, numbers, and underscores": "نام کاربری فقط می تواند شامل حروف، اعداد و زیرخط باشد",
	"password must be at least 6 characters":                      "رمز عبور باید حداقل ۶ کاراکتر باشد",
	"username already exists":                                     "این نام کاربری قبلا ثبت شده است",
	"invalid username or password":                                "نام کاربری یا رمز عبور اشتباه است",
}

var prefixTranslations = map[string]string{
	"failed to hash password:":   "خطا در پردازش رمز عبور",
	"failed to register user:":   "خطا در ثبت نام کاربر",
	"failed to get user id:":     "خطا در دریافت شناسه کاربر",
	"failed to query user:":      "خطا در دریافت اطلاعات کاربر",
	"failed to generate token:":  "خطا در تولید توکن",
	"failed to sign token:":      "خطا در امضای توکن",
	"failed to parse token:":     "توکن نامعتبر است",
	"unexpected signing method:": "روش امضای توکن نامعتبر است",
}

func Translate(message string) string {
	if translated, ok := translations[message]; ok {
		return translated
	}
	for prefix, translated := range prefixTranslations {
		if strings.HasPrefix(message, prefix) {
			return translated
		}
	}
	return message
}

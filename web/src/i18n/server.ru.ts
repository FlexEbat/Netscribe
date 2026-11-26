/*
 * Russian text for the sentences the server writes in English: settings, health checks, the
 * risk notes and the API reference. The key is the exact English sentence, so a sentence that is
 * not here shows in English and a test (server.test.ts) lists it. Sentences with a number or a
 * name inside are matched by the patterns in server.ts instead.
 */
export const serverRu: Record<string, string> = {
  "Tells whether the service is up. No sign-in.": "Показывает, работает ли служба. Вход не нужен.",
  "Signs in and sets the session cookie.": "Выполняет вход и ставит cookie сессии.",
  '{"username","password"}; with a second factor (when set up) the answer asks for it.':
    '{"username","password"}; если включён второй фактор, в ответе будет запрос кода.',
  "The user, and the cookie.": "Пользователь и cookie.",
  "Ends the session.": "Завершает сессию.",
  "The signed in user and what the caller may do.":
    "Вошедший пользователь и то, что ему разрешено.",
  "The user, the role and the permissions of the caller.": "Пользователь, его роль и права.",
  "Changes the password of the caller.": "Меняет пароль вызывающего.",
  "The second step of a sign in: the code from the authenticator app, or a recovery code.":
    "Второй шаг входа: код из приложения-аутентификатора или резервный код.",
  '{"challenge","code"}; the challenge comes from the answer of the login.':
    '{"challenge","code"}; challenge приходит в ответе на вход.',
  "Whether the second factor is on, and how many recovery codes are left.":
    "Включён ли второй фактор и сколько осталось резервных кодов.",
  "Starts the setup of a second factor. Nothing is on until a code is given to enable.":
    "Начинает настройку второго фактора. Пока код не передан в enable, ничего не включено.",
  '{"secret","uri"}: the secret to type and the otpauth address for a QR code.':
    '{"secret","uri"}: секрет для ввода вручную и адрес otpauth для QR-кода.',
  "Turns the second factor on with a code of the new secret.":
    "Включает второй фактор по коду нового секрета.",
  '{"recoveryCodes"}: ten codes, shown once.':
    '{"recoveryCodes"}: десять кодов, показываются один раз.',
  "Turns the second factor off.": "Выключает второй фактор.",
  '{"password","code"}; the code may be a recovery code.':
    '{"password","code"}; кодом может быть резервный код.',
  "Removes the second factor of a person who lost it and ends that person's sessions.":
    "Снимает второй фактор у человека, который его потерял, и завершает его сессии.",
  "Lists your own sessions, with the one of this request marked.":
    "Список ваших сессий; сессия этого запроса отмечена.",
  "A list with id, times, address, browser and current.":
    "Список с id, временем, адресом, браузером и признаком текущей.",
  "Ends one of your sessions. The session of another person is not found.":
    "Завершает одну из ваших сессий. Сессия другого человека не находится.",
  "Ends all your sessions but the one of this request.":
    "Завершает все ваши сессии, кроме сессии этого запроса.",
  '{"ended": how many}': '{"ended": сколько завершено}',
  "Which single sign-on buttons the sign in page shows. No sign-in.":
    "Какие кнопки единого входа показывает страница входа. Вход не нужен.",
  '{"oidc":{"name"}} when single sign-on is set up, else {}.':
    '{"oidc":{"name"}}, если единый вход настроен, иначе {}.',
  "Starts a single sign-on: sends the browser to the identity provider. No sign-in.":
    "Начинает единый вход: отправляет браузер к провайдеру. Вход не нужен.",
  "A redirect to the provider, and a short cookie that holds the state.":
    "Перенаправление к провайдеру и короткая cookie с состоянием.",
  "Where the provider sends the browser back. Checks the answer, opens a session and redirects to /. No sign-in.":
    "Сюда провайдер возвращает браузер. Проверяет ответ, открывает сессию и перенаправляет на /. Вход не нужен.",
  "must match the cookie": "должен совпадать с cookie",
  "the authorization code": "код авторизации",
  "A redirect to / with the session cookie, or to /?sso=failed.":
    "Перенаправление на / с cookie сессии или на /?sso=failed.",
  "The running version and whether a newer release exists.":
    "Запущенная версия и наличие более новой версии.",
  "Version, and the update state.": "Версия и состояние обновления.",
  "Lists the accounts.": "Список учётных записей.",
  "A list of users.": "Список пользователей.",
  "Creates an account.": "Создаёт учётную запись.",
  "Changes the role or the state of an account.": "Меняет роль или состояние учётной записи.",
  "Sets a new password for another account.": "Задаёт новый пароль для другой учётной записи.",
  "Deletes an account. The last administrator cannot be deleted.":
    "Удаляет учётную запись. Последнего администратора удалить нельзя.",
  "Lists your access tokens, never their secret part.":
    "Список ваших токенов доступа без секретной части.",
  "A list of tokens with name, scope and dates.": "Список токенов с названием, областью и датами.",
  "Creates an access token. The secret is shown once.":
    "Создаёт токен доступа. Секрет показывается один раз.",
  "The token, with its secret.": "Токен вместе с секретом.",
  "Revokes an access token.": "Отзывает токен доступа.",
  "Lets automatic updates go on after a rollback paused them.":
    "Возобновляет автоматические обновления, которые остановил откат.",
  "Looks for a newer release now.": "Сразу проверяет наличие новой версии.",
  "Asks the helper to install the newest release.": "Просит помощника установить новейшую версию.",
  "202 when the helper took the request.": "202, когда помощник принял запрос.",
  "Asks the helper to restart the service.": "Просит помощника перезапустить службу.",
  "The result of the last health checks, run the first time it is asked.":
    "Итог последних проверок состояния; при первом запросе проверки запускаются.",
  "The checks, when they ran, and the worst status.": "Проверки, время их запуска и худший статус.",
  "Runs the health checks again, at most once in 10 seconds.":
    "Запускает проверки состояния заново, не чаще раза в 10 секунд.",
  "The same as GET /api/health; 429 when it is too soon.":
    "То же, что GET /api/health; 429, если слишком рано.",
  "Downloads a backup: the database and the config with its secrets.":
    "Скачивает резервную копию: базу данных и конфиг с секретами.",
  "A .tar.gz file. The password of the caller goes in the X-Confirm-Password header, UTF-8 and base64 encoded.":
    "Файл .tar.gz. Пароль вызывающего передаётся в заголовке X-Confirm-Password в UTF-8 и base64.",
  "The certificate the panel is set to serve: its subject, names, dates and fingerprint, and whether it is self-signed. Never a key or a path.":
    "Сертификат, который панель должна отдавать: субъект, имена, даты и отпечаток, и самоподписан ли он. Никогда не ключ и не путь.",
  "Makes a self-signed certificate and sets tls.certFile and tls.keyFile. The password of the caller goes in the X-Confirm-Password header, UTF-8 and base64 encoded. 409 when a certificate is set and replace is not true.":
    "Создаёт самоподписанный сертификат и задаёт tls.certFile и tls.keyFile. Пароль вызывающего передаётся в заголовке X-Confirm-Password в UTF-8 и base64. 409, если сертификат уже задан, а replace не равен true.",
  '{"names":[],"days":365,"replace":false}; with no names the host name, localhost and the addresses of the host.':
    '{"names":[],"days":365,"replace":false}; без имён берутся имя хоста, localhost и адреса хоста.',
  '{"subject","names","notAfter","fingerprint","needRestart"}; the key is never sent.':
    '{"subject","names","notAfter","fingerprint","needRestart"}; ключ никогда не отправляется.',
  "Checks a backup and asks the helper to put it in place.":
    "Проверяет копию и просит помощника развернуть её.",
  "The .tar.gz file itself, with the password in the X-Confirm-Password header.":
    "Сам файл .tar.gz; пароль передаётся в заголовке X-Confirm-Password.",
  "202 and what the backup holds; the service restarts.":
    "202 и состав копии; служба перезапускается.",
  "The settings, their values in use and which need a restart.":
    "Настройки, их текущие значения и те, что требуют перезапуска.",
  "Changes settings.": "Меняет настройки.",
  "An object of setting names and new values.": "Объект: названия настроек и новые значения.",
  "The notification channels and the events they are told about.":
    "Каналы уведомлений и события, о которых они сообщают.",
  "Sends a test message to the channels.": "Отправляет в каналы проверочное сообщение.",
  "The audit log, newest first.": "Журнал аудита, сначала новые записи.",
  "how many entries": "сколько записей",
  "only entries older than this id": "только записи старше этого id",
  "only ok, denied or error": "только ok, denied или error",
  "Containers found on the Docker host.": "Контейнеры, найденные на хосте Docker.",
  "Counts of devices, risky services and scans in the Prometheus text format. Answers 404 unless metrics.enabled is on.":
    "Счётчики устройств, опасных служб и сканирований в текстовом формате Prometheus. Отвечает 404, пока не включён metrics.enabled.",
  "A document of the network in the given format.": "Документ о сети в выбранном формате.",
  "The file.": "Файл.",
  "What changed between scans.": "Что изменилось между сканированиями.",
  "how many changes": "сколько изменений",
  "Lists devices.": "Список устройств.",
  "text to look for": "текст для поиска",
  "only this kind": "только этот тип",
  "true or false": "true или false",
  "only this group id": "только группа с этим id",
  "Adds a device by hand.": "Добавляет устройство вручную.",
  "One device with its services.": "Одно устройство с его службами.",
  "When the device was seen and what changed.": "Когда устройство видели и что изменилось.",
  "Changes the name, kind, notes, trust or group of a device.":
    "Меняет название, тип, заметки, доверие или группу устройства.",
  "Deletes a device.": "Удаляет устройство.",
  "Changes up to 200 devices at once.": "Меняет до 200 устройств за раз.",
  "true to only say what would change": "true: только сообщить, что изменится",
  "Imports devices from CSV.": "Импортирует устройства из CSV.",
  "Saves where a device sits on the topology canvas.":
    "Сохраняет положение устройства на холсте топологии.",
  "Asks one device again what it is.": "Заново определяет, что это за устройство.",
  "Sends a Wake on LAN packet to one device.": "Отправляет устройству пакет Wake on LAN.",
  "Counts for the overview page.": "Счётчики для страницы обзора.",
  "Devices by presence, risky services, recent scans.":
    "Устройства по наличию в сети, опасные службы, последние сканирования.",
  "Lists groups.": "Список групп.",
  "Creates a group.": "Создаёт группу.",
  "Renames or recolors a group.": "Переименовывает группу или меняет её цвет.",
  "Deletes a group.": "Удаляет группу.",
  "Lists your saved filters.": "Список ваших сохранённых фильтров.",
  "Saves a filter. It is yours: it needs only the right to read.":
    "Сохраняет фильтр. Он ваш: нужно только право чтения.",
  "Deletes one of your saved filters.": "Удаляет один из ваших сохранённых фильтров.",
  "Draws a link between two devices by hand.": "Вручную рисует связь между двумя устройствами.",
  "Deletes a link made by hand.": "Удаляет связь, созданную вручную.",
  "Devices and links for the canvas.": "Устройства и связи для холста.",
  "Open services.": "Открытые службы.",
  "only this device": "только это устройство",
  "true for services with a risk note": "true: только службы с пометкой о риске",
  "Lists scans, newest first.": "Список сканирований, сначала новые.",
  "One scan.": "Одно сканирование.",
  "Starts a scan.": "Запускает сканирование.",
  "202 and the scan that started.": "202 и запущенное сканирование.",
  "Whether nmap is found and used.": "Найден ли nmap и используется ли он.",
  "A stream of events (server sent events): scans started and finished, devices changed.":
    "Поток событий (server-sent events): сканирования начались и закончились, устройства изменились.",
  "This reference as an OpenAPI 3.1 document, with only the routes the caller may use.":
    "Этот справочник в виде документа OpenAPI 3.1, только с маршрутами, доступными вызывающему.",
  "Success.": "Успешно.",
  "An access token from the Tokens page.": "Токен доступа со страницы «Токены».",
  Scanning: "Сканирование",
  "what is scanned and how often": "что сканируется и как часто",
  "Networks to scan": "Сети для сканирования",
  "Private networks in CIDR form, a /16 at most. ARP discovery sees the networks this host is connected to.":
    "Частные сети в формате CIDR, не крупнее /16. ARP-обнаружение видит сети, к которым подключён этот хост.",
  "Scan every": "Сканировать каждые",
  "A duration such as 15m or 1h, at least 1m. 0 turns the schedule off: scans run only on request.":
    "Интервал, например 15m или 1h, не меньше 1m. 0 отключает расписание: сканирование идёт только по запросу.",
  "TCP ports to probe": "TCP-порты для проверки",
  "Ports that decide what a host is: 22, 80, 443 and so on. A longer list makes a scan slower.":
    "Порты, по которым определяется тип хоста: 22, 80, 443 и так далее. Чем длиннее список, тем медленнее сканирование.",
  "Wait for a port": "Ожидание порта",
  "How long one TCP connection may take before the port counts as closed, 100ms to 10s. Raise it on a slow or distant network.":
    "Сколько может длиться одно TCP-соединение, прежде чем порт сочтут закрытым: от 100ms до 10s. Увеличьте для медленной или удалённой сети.",
  "Connections at once": "Одновременных соединений",
  "How many TCP connections the port scan holds open together, 1 to 512. Lower it on a small host or a weak link.":
    "Сколько TCP-соединений сканер портов держит открытыми одновременно: от 1 до 512. Уменьшите на слабом хосте или канале.",
  "Try again after a failed scheduled scan": "Повтор после неудачного сканирования по расписанию",
  "Wait this long, 1m to 24h, then run the scheduled scan again until it succeeds. 0 turns the retry off.":
    "Подождать столько (от 1m до 24h) и повторять сканирование по расписанию, пока оно не удастся. 0 отключает повтор.",
  "Allow public networks as targets": "Разрешить публичные сети как цели",
  "Off keeps the scanner inside private ranges. Turn it on only for networks you own.":
    "Выключено: сканер остаётся в частных диапазонах. Включайте только для сетей, которые вам принадлежат.",
  "Probe networks behind a router": "Проверять сети за маршрутизатором",
  "Lets targets that this host does not touch directly be probed with TCP and ICMP.":
    "Позволяет проверять по TCP и ICMP цели, к которым этот хост не подключён напрямую.",
  Discovery: "Обнаружение",
  "how devices are told apart": "как различаются устройства",
  "Identify devices with Nmap": "Определять устройства через Nmap",
  "Asks an installed nmap about devices that nothing else could type. Needs raw socket rights for the service.":
    "Спрашивает установленный nmap об устройствах, которые нечем определить иначе. Службе нужны права на raw-сокеты.",
  "Which devices Nmap asks about": "О каких устройствах спрашивает Nmap",
  "untyped: only devices nothing else could type. all: every device, slower and noisier.":
    "untyped: только устройства, которые нечем определить иначе. all: все устройства, медленнее и шумнее.",
  "One PTR query in each local segment.": "Один PTR-запрос в каждом локальном сегменте.",
  "One multicast search in each local segment.":
    "Один multicast-поиск в каждом локальном сегменте.",
  "HTTP banners": "HTTP-баннеры",
  "A HEAD / request on the open web ports.": "Запрос HEAD / на открытых веб-портах.",
  "Router tables over SNMP": "Таблицы маршрутизаторов по SNMP",
  "Reads ARP and bridge tables of routers and switches.":
    "Читает ARP- и мостовые таблицы маршрутизаторов и коммутаторов.",
  "Path of nmap": "Путь к nmap",
  "An absolute path of the nmap program. Empty means look for nmap in PATH.":
    "Абсолютный путь к программе nmap. Пусто: искать nmap в PATH.",
  "IPv6 discovery": "Обнаружение IPv6",
  "One echo to ff02::1 in each scanned segment, then a read of the neighbour table.":
    "Один echo-запрос на ff02::1 в каждом сканируемом сегменте, затем чтение таблицы соседей.",
  "TLS certificates": "TLS-сертификаты",
  "A handshake, and no data, on the open TLS ports to read the certificate.":
    "Рукопожатие без передачи данных на открытых TLS-портах, чтобы прочитать сертификат.",
  "Listen for DHCP requests": "Слушать DHCP-запросы",
  "Reads the DHCP requests other machines broadcast and never answers. Needs raw socket rights for the service.":
    "Читает DHCP-запросы, которые рассылают другие машины, и никогда не отвечает. Службе нужны права на raw-сокеты.",
  Docker: "Docker",
  "the container view": "обзор контейнеров",
  "Read containers from Docker": "Читать контейнеры из Docker",
  "The service reads container data through the Docker socket. It needs read access to the socket.":
    "Служба читает данные о контейнерах через сокет Docker. Ей нужен доступ на чтение к сокету.",
  "Docker socket": "Сокет Docker",
  "The path of the Unix socket.": "Путь к Unix-сокету.",
  "device queries": "запросы к устройствам",
  Communities: "Community-строки",
  "Read only community strings, tried in order. Use a community that the devices restrict by ACL, not public.":
    "Community-строки только для чтения, пробуются по порядку. Используйте community, ограниченную ACL на устройствах, а не public.",
  "Timeout of a query": "Тайм-аут запроса",
  "How long one SNMP request waits.": "Сколько ждёт один SNMP-запрос.",
  History: "История",
  "how long scans and the audit log are kept": "как долго хранятся сканирования и журнал аудита",
  "Scans to keep": "Сколько сканирований хранить",
  "Older scans are pruned.": "Более старые сканирования удаляются.",
  "Days to keep the audit log": "Дней хранения журнала аудита",
  "Entries of the audit log older than this are pruned.":
    "Записи журнала аудита старше этого срока удаляются.",
  "Sign in policy": "Политика входа",
  "accounts and sessions": "учётные записи и сессии",
  "Shortest password": "Минимальная длина пароля",
  "Longer is better. Twelve is the recommended minimum.":
    "Чем длиннее, тем лучше. Рекомендуемый минимум: двенадцать.",
  "Failed attempts before a lockout": "Неудачных попыток до блокировки",
  "Counted per address and username.":
    "Считается отдельно для каждой пары адрес и имя пользователя.",
  "Lockout time": "Время блокировки",
  "How long a locked pair has to wait.": "Сколько должна ждать заблокированная пара.",
  "Idle session timeout": "Тайм-аут неактивной сессии",
  "A session with no request for this long ends.":
    "Сессия без запросов в течение этого времени завершается.",
  "Longest session": "Максимальная длительность сессии",
  "A session ends after this time, active or not.":
    "Сессия завершается по истечении этого времени, активна она или нет.",
  Export: "Экспорт",
  "reports and metrics": "отчёты и метрики",
  "Browser for PDF export": "Браузер для экспорта в PDF",
  "The path of chromium or chrome. Empty means look for one in PATH.":
    "Путь к chromium или chrome. Пусто: искать в PATH.",
  "PDF without Chrome's sandbox": "PDF без песочницы Chrome",
  "Off tries the sandbox first and, where the system forbids it (root, a container, no user namespaces), prints again without it: the failed try costs about ten seconds once after each start. On skips it. The page is made by Netscribe with every value escaped, and the browser has no script and no network, but the sandbox is one more wall: turn this on only where the sandbox cannot work.":
    "Выключено: сначала пробуется песочница, и если система её запрещает (root, контейнер, нет пользовательских пространств имён), печать повторяется без неё: неудачная попытка стоит около десяти секунд один раз после каждого запуска. Включено: песочница пропускается. Страницу делает Netscribe, все значения экранируются, у браузера нет ни скриптов, ни сети, но песочница — ещё одна стена: включайте только там, где она не может работать.",
  "Serve /metrics": "Отдавать /metrics",
  "Prometheus text format with counts of devices, risky services and scans. Reading it needs a token with the read scope. Names and addresses of devices are never in it.":
    "Текстовый формат Prometheus со счётчиками устройств, опасных служб и сканирований. Для чтения нужен токен с областью read. Названий и адресов устройств в нём никогда нет.",
  SSH: "SSH",
  "reading hosts over SSH (the hosts themselves: SSH hosts)":
    "чтение хостов по SSH (сами хосты: SSH-хосты)",
  "known_hosts file": "Файл known_hosts",
  "Strict: a host that is not listed, or whose key changed, is skipped. Fill it with ssh-keyscan and check the fingerprint.":
    "Строго: хост, которого нет в списке или у которого сменился ключ, пропускается. Заполните файл через ssh-keyscan и проверьте отпечаток.",
  Updates: "Обновления",
  "newer releases": "новые версии",
  "Look for new releases": "Искать новые версии",
  "Asks GitHub every few hours whether a newer release exists. Nothing about this host is sent. Off hides the update notice.":
    "Раз в несколько часов спрашивает GitHub, есть ли новая версия. Ничего об этом хосте не отправляется. Выключено: уведомление об обновлении скрыто.",
  "Install updates by itself": "Устанавливать обновления самостоятельно",
  "patch installs a newer release with the same major and minor number, minor one with the same major number. A new major version is never installed by itself. The service must look for releases, and a root helper must be installed. If the new version does not answer in a minute, the old program is put back and this stays paused until you resume it.":
    "patch ставит новую версию с теми же основным и вторым номерами, minor — с тем же основным номером. Новая основная версия сама не ставится. Служба должна искать версии, а root-помощник должен быть установлен. Если новая версия не ответит за минуту, возвращается старая программа, и автообновление стоит на паузе, пока вы его не возобновите.",
  "Window for automatic updates": "Окно автоматических обновлений",
  "Local time, from-to, for example 03:00-05:00. It may cross midnight. An update starts only inside the window, and never while a scan runs.":
    "Местное время, от и до, например 03:00-05:00. Окно может переходить через полночь. Обновление начинается только внутри окна и никогда во время сканирования.",
  "Telnet sends passwords in clear text": "Telnet передаёт пароли открытым текстом",
  "TFTP has no authentication": "В TFTP нет аутентификации",
  "VNC is often open without a strong password": "VNC часто открыт без надёжного пароля",
  "FTP sends passwords in clear text": "FTP передаёт пароли открытым текстом",
  "SMB is open, check that old versions are off":
    "SMB открыт, проверьте, что старые версии отключены",
  "RDP is open, keep it off the open network": "RDP открыт, не выставляйте его в открытую сеть",
  "SNMP is open, check the community and the ACL": "SNMP открыт, проверьте community и ACL",
  "the check stopped with an internal error": "проверка остановилась из-за внутренней ошибки",
  "Look at the log of the service.": "Посмотрите журнал службы.",
  "the check did not finish in 5 seconds": "проверка не завершилась за 5 секунд",
  "the check was cancelled": "проверка отменена",
  "the file could not be read here": "здесь не удалось прочитать файл",
  "every user on this host can read the file, and it holds secrets":
    "любой пользователь этого хоста может прочитать файл, а в нём есть секреты",
  "only the owner and the group can read it": "читать файл могут только владелец и группа",
  "the free space could not be read": "не удалось узнать свободное место",
  "Check the state directory by hand.": "Проверьте каталог состояния вручную.",
  "Free space, or lower history.keepScans and audit.keepDays.":
    "Освободите место или уменьшите history.keepScans и audit.keepDays.",
  "no network is set, so a scan only sees what this host is connected to":
    "сеть не задана, поэтому сканирование видит только то, к чему подключён этот хост",
  "Set scan.targets on the Settings page.": "Задайте scan.targets на странице «Настройки».",
  "no scan has run yet": "сканирований ещё не было",
  "Run a scan from the Overview page.": "Запустите сканирование на странице «Обзор».",
  "the latest scan failed": "последнее сканирование завершилось ошибкой",
  "Open the scan on the Overview page, and run netscribe doctor on the host.":
    "Откройте сканирование на странице «Обзор» и запустите netscribe doctor на хосте.",
  "a scan is running, or scheduled scans are off":
    "идёт сканирование или сканирование по расписанию выключено",
  "the latest scan finished on time": "последнее сканирование завершилось вовремя",
  "Check that the service runs: sudo systemctl status netscribe":
    "Проверьте, что служба работает: sudo systemctl status netscribe",
  "discovery.nmap is off": "discovery.nmap выключен",
  "discovery.nmap is on but nmap was not found": "discovery.nmap включён, но nmap не найден",
  "Install nmap, or set discovery.nmapPath.": "Установите nmap или задайте discovery.nmapPath.",
  "nmap was found": "nmap найден",
  "export.chromePath does not exist": "export.chromePath не существует",
  "Set export.chromePath to a browser, or empty it.":
    "Укажите в export.chromePath браузер или очистите значение.",
  "the browser of export.chromePath exists": "браузер из export.chromePath существует",
  "no browser was found: PDF export is not available, the other formats are":
    "браузер не найден: экспорт в PDF недоступен, остальные форматы доступны",
  "docker.enabled is off": "docker.enabled выключен",
  "docker.enabled is on but the socket cannot be reached":
    "docker.enabled включён, но сокет недоступен",
  "Check docker.socket, and that the service account may use it.":
    "Проверьте docker.socket и что учётной записи службы разрешено его использовать.",
  "the socket is there": "сокет на месте",
  "the panel does not serve TLS itself": "панель сама не обслуживает TLS",
  "the certificate file cannot be read": "файл сертификата не читается",
  "Check tls.certFile and its permissions.": "Проверьте tls.certFile и его права.",
  "the certificate file holds no certificate": "в файле сертификата нет сертификата",
  "Check tls.certFile.": "Проверьте tls.certFile.",
  "the certificate cannot be read": "сертификат не читается",
  "Renew the certificate and restart the service.": "Обновите сертификат и перезапустите службу.",
  "the update helper is installed": "помощник обновлений установлен",
  "no update helper: the panel shows the update command instead of a button":
    "помощника обновлений нет: вместо кнопки панель показывает команду обновления",
  "the message was not delivered:": "сообщение не доставлено:",
  "the password is missing: send it in the": "пароль не передан: отправьте его в заголовке",
  "the first line must name the columns": "первая строка должна называть столбцы",
  "the file is not valid CSV": "файл не является корректным CSV",
  "rolled back": "откат выполнен",
  "a second factor is needed": "нужен второй фактор",
  "authentication required": "требуется вход",
  "current password is incorrect": "текущий пароль неверен",
  "device has no ip, mac or label": "у устройства нет ни IP, ни MAC, ни названия",
  "groupId must be 0 or a group id": "groupId должен быть 0 или id группы",
  "hostname is longer than 255 characters": "имя хоста длиннее 255 символов",
  "invalid username or password": "неверное имя пользователя или пароль",
  "ip is not valid": "некорректный IP-адрес",
  "ip6 is not a valid IPv6 address": "ip6 не является корректным адресом IPv6",
  "kind is unknown": "неизвестный тип",
  "label is longer than 100 characters": "название длиннее 100 символов",
  "link connects a device to itself": "связь соединяет устройство с самим собой",
  "mac is not valid": "некорректный MAC-адрес",
  "notes are longer than 2000 characters": "заметки длиннее 2000 символов",
  "password equals username": "пароль совпадает с именем пользователя",
  "password is longer than 128 characters": "пароль длиннее 128 символов",
  "password is shorter than the minimum": "пароль короче минимальной длины",
  "password is too common": "пароль слишком распространённый",
  "port is longer than 64 characters": "порт длиннее 64 символов",
  "role is unknown": "неизвестная роль",
  "scope is above your role": "область выше вашей роли",
  "scope is unknown": "неизвестная область",
  "the group does not exist": "группа не существует",
  "the helper is busy with an earlier request": "помощник занят предыдущим запросом",
  "the new password must differ from the current one": "новый пароль должен отличаться от текущего",
  "token lifetime is out of range": "срок жизни токена вне допустимого диапазона",
  "token name is invalid": "недопустимое название токена",
  "username is invalid": "недопустимое имя пользователя",
  "a device with this MAC or address already exists":
    "устройство с таким MAC или адресом уже существует",
  "a filter must filter something": "фильтр должен что-то фильтровать",
  "name must be 1 to 40 characters without control characters":
    "название: от 1 до 40 символов без управляющих",
  "page must be inventory, services or containers":
    "страница должна быть inventory, services или containers",
  "query must be a query string of at most 300 characters":
    "query должен быть строкой запроса не длиннее 300 символов",
  "description must be at most 200 characters without control characters":
    "описание: не больше 200 символов без управляющих",
  "a group has at most 16 ranges": "в группе не больше 16 диапазонов",
  "vlan must be a number from 1 to 4094": "vlan должен быть числом от 1 до 4094",
  "cannot remove the last administrator": "нельзя удалить последнего администратора",
  "the link was found by a scan, not drawn by hand":
    "связь найдена сканированием, а не нарисована вручную",
  "the record was changed by someone else": "запись изменил кто-то другой",
  "group must be none or a group id": "group должен быть none или id группы",
  "q must be at most 100 characters without control characters":
    "q: не больше 100 символов без управляющих",
  "status must be online or offline": "status должен быть online или offline",
  "trust must be unknown or trusted": "trust должен быть unknown или trusted",
}

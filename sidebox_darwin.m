//go:build darwin

#import <Cocoa/Cocoa.h>
#import <ServiceManagement/ServiceManagement.h>
#import "sidebox_darwin.h"

static NSDictionary *SideboxParseJSON(const char *value) {
    if (value == NULL) {
        return @{};
    }
    NSData *data = [[NSString stringWithUTF8String:value] dataUsingEncoding:NSUTF8StringEncoding];
    if (data == nil) {
        return @{};
    }
    id object = [NSJSONSerialization JSONObjectWithData:data options:0 error:nil];
    return [object isKindOfClass:[NSDictionary class]] ? object : @{};
}

static NSString *SideboxString(NSDictionary *dictionary, NSString *key) {
    id value = dictionary[key];
    return [value isKindOfClass:[NSString class]] ? value : @"";
}

static CGFloat SideboxNumber(NSDictionary *dictionary, NSString *key, CGFloat fallback) {
    id value = dictionary[key];
    return [value isKindOfClass:[NSNumber class]] ? [value doubleValue] : fallback;
}

@interface SideboxView : NSView
@property(nonatomic, copy) NSDictionary *config;
@property(nonatomic, copy) NSDictionary *weather;
@property(nonatomic, copy) NSString *configPath;
@property(nonatomic, copy) NSString *version;
@property(nonatomic, copy) NSString *weatherError;
@property(nonatomic) BOOL loadingWeather;
@property(nonatomic, strong) NSTimer *clockTimer;
@property(nonatomic, strong) NSTimer *weatherTimer;
- (instancetype)initWithFrame:(NSRect)frame
                       config:(NSDictionary *)config
                   configPath:(NSString *)configPath
                      version:(NSString *)version;
- (void)start;
- (void)applyConfig:(NSDictionary *)config;
- (void)refreshWeather:(id)sender;
- (void)syncStartAtLoginFromConfig;
@end

@interface SideboxAppDelegate : NSObject <NSApplicationDelegate, NSWindowDelegate>
@property(nonatomic, strong) NSWindow *window;
@property(nonatomic, strong) SideboxView *sideboxView;
@property(nonatomic, copy) NSDictionary *initialConfig;
@property(nonatomic, copy) NSString *configPath;
@property(nonatomic, copy) NSString *version;
- (instancetype)initWithConfig:(NSDictionary *)config
                     configPath:(NSString *)configPath
                        version:(NSString *)version;
@end

@implementation SideboxView

- (instancetype)initWithFrame:(NSRect)frame
                       config:(NSDictionary *)config
                   configPath:(NSString *)configPath
                      version:(NSString *)version {
    self = [super initWithFrame:frame];
    if (self != nil) {
        _config = [config copy];
        _configPath = [configPath copy];
        _version = [version copy];
        _weather = @{};
        _weatherError = @"";
    }
    return self;
}

- (BOOL)isFlipped {
    return YES;
}

- (BOOL)isOpaque {
    return NO;
}

- (void)start {
    __weak SideboxView *weakSelf = self;
    self.clockTimer = [NSTimer scheduledTimerWithTimeInterval:1.0 repeats:YES block:^(NSTimer *timer) {
        (void)timer;
        [weakSelf setNeedsDisplay:YES];
    }];
    [[[NSWorkspace sharedWorkspace] notificationCenter]
        addObserver:self
           selector:@selector(refreshWeather:)
               name:NSWorkspaceDidWakeNotification
             object:nil];
    [self applyConfig:self.config];
    [self refreshWeather:nil];
}

- (void)dealloc {
    [self.clockTimer invalidate];
    [self.weatherTimer invalidate];
    [[[NSWorkspace sharedWorkspace] notificationCenter] removeObserver:self];
}

- (void)applyConfig:(NSDictionary *)config {
    self.config = config;
    [self.weatherTimer invalidate];
    NSInteger minutes = MAX(5, (NSInteger)SideboxNumber(config, @"refresh_minutes", 15));
    __weak SideboxView *weakSelf = self;
    self.weatherTimer = [NSTimer scheduledTimerWithTimeInterval:minutes * 60.0 repeats:YES block:^(NSTimer *timer) {
        (void)timer;
        [weakSelf refreshWeather:nil];
    }];
    [self setNeedsDisplay:YES];
}

- (void)refreshWeather:(id)sender {
    (void)sender;
    if (self.loadingWeather) {
        return;
    }
    self.loadingWeather = YES;
    self.weatherError = @"";
    [self setNeedsDisplay:YES];

    __weak SideboxView *weakSelf = self;
    dispatch_async(dispatch_get_global_queue(QOS_CLASS_UTILITY, 0), ^{
        char *json = SideboxFetchWeatherJSON();
        NSDictionary *result = SideboxParseJSON(json);
        free(json);
        dispatch_async(dispatch_get_main_queue(), ^{
            SideboxView *view = weakSelf;
            if (view == nil) {
                return;
            }
            view.loadingWeather = NO;
            NSString *error = SideboxString(result, @"error");
            if (error.length > 0) {
                view.weatherError = error;
            } else {
                view.weather = result;
                view.weatherError = @"";
            }
            [view setNeedsDisplay:YES];
        });
    });
}

- (void)reloadConfig:(id)sender {
    (void)sender;
    char *json = SideboxReloadConfigJSON();
    NSDictionary *result = SideboxParseJSON(json);
    free(json);

    NSString *error = SideboxString(result, @"error");
    if (error.length > 0) {
        NSAlert *alert = [[NSAlert alloc] init];
        alert.messageText = @"設定を再読込できません";
        alert.informativeText = error;
        [alert runModal];
        return;
    }
    [self applyConfig:result];
    CGFloat opacity = SideboxNumber(result, @"opacity", 0.94);
    self.window.alphaValue = MIN(1.0, MAX(0.35, opacity));
    self.window.level = [result[@"always_on_top"] boolValue] ? NSFloatingWindowLevel : NSNormalWindowLevel;
    [self syncStartAtLoginFromConfig];
    [self refreshWeather:nil];
}

- (void)openConfig:(id)sender {
    (void)sender;
    NSURL *url = [NSURL fileURLWithPath:self.configPath];
    [[NSWorkspace sharedWorkspace] openURL:url];
}

- (void)quitApplication:(id)sender {
    (void)sender;
    [NSApp terminate:nil];
}

- (void)showStartAtLoginError:(NSString *)message {
    NSAlert *alert = [[NSAlert alloc] init];
    alert.messageText = @"自動起動を設定できません";
    alert.informativeText = message;
    [alert runModal];
}

- (BOOL)persistStartAtLogin:(BOOL)enabled {
    char *json = SideboxSetStartAtLogin(enabled ? 1 : 0);
    NSDictionary *result = SideboxParseJSON(json);
    free(json);
    NSString *error = SideboxString(result, @"error");
    if (error.length > 0) {
        [self showStartAtLoginError:error];
        return NO;
    }
    self.config = result;
    return YES;
}

- (void)syncStartAtLoginFromConfig {
    if (@available(macOS 13.0, *)) {
        SMAppService *service = SMAppService.mainAppService;
        BOOL desired = [self.config[@"start_at_login"] boolValue];
        if (service.status == SMAppServiceStatusEnabled) {
            if (!desired) {
                NSError *error = nil;
                if (![service unregisterAndReturnError:&error]) {
                    [self showStartAtLoginError:error.localizedDescription ?: @"登録解除に失敗しました。"];
                }
            }
            return;
        }
        if (service.status == SMAppServiceStatusRequiresApproval && !desired) {
            NSError *error = nil;
            if (![service unregisterAndReturnError:&error]) {
                [self showStartAtLoginError:error.localizedDescription ?: @"登録解除に失敗しました。"];
            }
            return;
        }
        if (service.status == SMAppServiceStatusNotRegistered && desired) {
            NSError *error = nil;
            if (![service registerAndReturnError:&error]) {
                [self showStartAtLoginError:error.localizedDescription ?: @"登録に失敗しました。"];
            }
        } else if (service.status == SMAppServiceStatusNotFound && desired) {
            [self showStartAtLoginError:@"Sidebox.appをApplicationsフォルダへ移動してから、もう一度設定してください。"];
        }
    }
}

- (void)toggleStartAtLogin:(id)sender {
    (void)sender;
    if (@available(macOS 13.0, *)) {
        SMAppService *service = SMAppService.mainAppService;
        if (service.status == SMAppServiceStatusRequiresApproval) {
            [SMAppService openSystemSettingsLoginItems];
            return;
        }

        BOOL enable = service.status != SMAppServiceStatusEnabled;
        NSError *error = nil;
        BOOL changed = enable
            ? [service registerAndReturnError:&error]
            : [service unregisterAndReturnError:&error];
        if (!changed) {
            [self showStartAtLoginError:error.localizedDescription ?: @"設定の変更に失敗しました。"];
            return;
        }
        if (![self persistStartAtLogin:enable]) {
            if (enable) {
                [service unregisterAndReturnError:nil];
            } else {
                [service registerAndReturnError:nil];
            }
        }
        return;
    }
    [self showStartAtLoginError:@"この機能にはmacOS 13以降が必要です。"];
}

- (NSMenu *)menuForEvent:(NSEvent *)event {
    (void)event;
    NSMenu *menu = [[NSMenu alloc] initWithTitle:@"Sidebox"];
    [menu addItemWithTitle:@"天気を更新" action:@selector(refreshWeather:) keyEquivalent:@""];
    [menu addItemWithTitle:@"設定を再読込" action:@selector(reloadConfig:) keyEquivalent:@""];
    [menu addItemWithTitle:@"設定ファイルを開く" action:@selector(openConfig:) keyEquivalent:@""];
    NSMenuItem *startupItem = [menu addItemWithTitle:@"ログイン時に開く"
                                              action:@selector(toggleStartAtLogin:)
                                       keyEquivalent:@""];
    if (@available(macOS 13.0, *)) {
        switch (SMAppService.mainAppService.status) {
        case SMAppServiceStatusEnabled:
            startupItem.state = NSControlStateValueOn;
            break;
        case SMAppServiceStatusRequiresApproval:
            startupItem.state = NSControlStateValueMixed;
            startupItem.toolTip = @"システム設定での承認が必要です";
            break;
        case SMAppServiceStatusNotFound:
            startupItem.title = @"ログイン時に開く（要インストール）";
            startupItem.state = NSControlStateValueOff;
            break;
        default:
            startupItem.state = NSControlStateValueOff;
            break;
        }
    }
    [menu addItem:[NSMenuItem separatorItem]];
    [menu addItemWithTitle:@"終了" action:@selector(quitApplication:) keyEquivalent:@""];
    for (NSMenuItem *item in menu.itemArray) {
        item.target = self;
    }
    return menu;
}

- (NSString *)weatherSymbolName:(NSString *)description {
    if ([description containsString:@"雷"]) return @"cloud.bolt.rain.fill";
    if ([description containsString:@"雪"]) return @"cloud.snow.fill";
    if ([description containsString:@"雨"]) return @"cloud.rain.fill";
    if ([description containsString:@"霧"]) return @"cloud.fog.fill";
    if ([description containsString:@"晴"] && [description containsString:@"曇"]) return @"cloud.sun.fill";
    if ([description containsString:@"晴"]) return @"sun.max.fill";
    if ([description containsString:@"曇"]) return @"cloud.fill";
    return @"questionmark";
}

- (NSColor *)weatherSymbolColor:(NSString *)description {
    if ([description containsString:@"雷"]) return [NSColor colorWithRed:1.0 green:0.72 blue:0.22 alpha:1];
    if ([description containsString:@"雨"]) return [NSColor colorWithRed:0.34 green:0.76 blue:1.0 alpha:1];
    if ([description containsString:@"雪"]) return [NSColor colorWithRed:0.78 green:0.91 blue:1.0 alpha:1];
    if ([description containsString:@"晴"]) return [NSColor colorWithRed:1.0 green:0.72 blue:0.22 alpha:1];
    return [NSColor colorWithWhite:0.78 alpha:1];
}

- (void)drawWeatherIcon:(NSString *)description inRect:(NSRect)rect primary:(BOOL)primary {
    NSImage *image = [NSImage imageWithSystemSymbolName:[self weatherSymbolName:description]
                              accessibilityDescription:description];
    if (image == nil) {
        [self drawText:@"–" inRect:rect size:36 weight:NSFontWeightRegular
                  color:NSColor.whiteColor alignment:NSTextAlignmentCenter];
        return;
    }
    NSImageSymbolConfiguration *configuration =
        [NSImageSymbolConfiguration configurationWithPointSize:primary ? 45 : 39
                                                        weight:NSFontWeightMedium];
    NSColor *symbolColor = [self weatherSymbolColor:description];
    if (@available(macOS 12.0, *)) {
        NSImageSymbolConfiguration *colorConfiguration =
            [NSImageSymbolConfiguration configurationWithHierarchicalColor:symbolColor];
        configuration = [configuration configurationByApplyingConfiguration:colorConfiguration];
    }
    image = [image imageWithSymbolConfiguration:configuration];
    if (@available(macOS 12.0, *)) {
        [image drawInRect:rect
                 fromRect:NSZeroRect
                operation:NSCompositingOperationSourceOver
                 fraction:1.0
           respectFlipped:YES
                    hints:nil];
        return;
    }

    NSImage *tintedImage = [[NSImage alloc] initWithSize:image.size];
    [tintedImage lockFocus];
    [image drawInRect:NSMakeRect(0, 0, image.size.width, image.size.height)
             fromRect:NSZeroRect
            operation:NSCompositingOperationSourceOver
             fraction:1.0];
    [symbolColor setFill];
    NSRectFillUsingOperation(NSMakeRect(0, 0, image.size.width, image.size.height),
                             NSCompositingOperationSourceAtop);
    [tintedImage unlockFocus];
    [NSGraphicsContext saveGraphicsState];
    [tintedImage drawInRect:rect
             fromRect:NSZeroRect
            operation:NSCompositingOperationSourceOver
             fraction:1.0
       respectFlipped:YES
                hints:nil];
    [NSGraphicsContext restoreGraphicsState];
}

- (void)drawText:(NSString *)text
           inRect:(NSRect)rect
             size:(CGFloat)size
           weight:(NSFontWeight)weight
            color:(NSColor *)color
        alignment:(NSTextAlignment)alignment {
    if (text.length == 0) {
        return;
    }
    NSMutableParagraphStyle *style = [[NSMutableParagraphStyle alloc] init];
    style.alignment = alignment;
    style.lineBreakMode = NSLineBreakByTruncatingTail;
    NSDictionary *attributes = @{
        NSFontAttributeName: [NSFont systemFontOfSize:size weight:weight],
        NSForegroundColorAttributeName: color,
        NSParagraphStyleAttributeName: style,
    };
    [text drawInRect:rect withAttributes:attributes];
}

- (void)drawMultilineText:(NSString *)text
                    inRect:(NSRect)rect
                      size:(CGFloat)size
                    weight:(NSFontWeight)weight
                     color:(NSColor *)color
                 alignment:(NSTextAlignment)alignment {
    if (text.length == 0) {
        return;
    }
    NSMutableParagraphStyle *style = [[NSMutableParagraphStyle alloc] init];
    style.alignment = alignment;
    style.lineBreakMode = NSLineBreakByCharWrapping;
    style.lineSpacing = 2;
    NSDictionary *attributes = @{
        NSFontAttributeName: [NSFont systemFontOfSize:size weight:weight],
        NSForegroundColorAttributeName: color,
        NSParagraphStyleAttributeName: style,
    };
    [text drawWithRect:rect
               options:NSStringDrawingUsesLineFragmentOrigin | NSStringDrawingTruncatesLastVisibleLine
            attributes:attributes];
}

- (void)drawClock:(NSString *)text inRect:(NSRect)rect {
    NSDictionary *attributes = @{
        NSFontAttributeName: [NSFont monospacedDigitSystemFontOfSize:50 weight:NSFontWeightLight],
        NSForegroundColorAttributeName: NSColor.whiteColor,
    };
    [text drawInRect:rect withAttributes:attributes];
}

- (NSString *)degreeValue:(id)value {
    return [value isKindOfClass:[NSNumber class]]
        ? [NSString stringWithFormat:@"%.0f°", [value doubleValue]] : @"--°";
}

- (NSString *)percentValue:(id)value {
    return [value isKindOfClass:[NSNumber class]]
        ? [NSString stringWithFormat:@"%@%%", value] : @"--%";
}

- (void)drawMetric:(NSString *)label
              value:(NSString *)value
             inRect:(NSRect)rect
              color:(NSColor *)color
            primary:(BOOL)primary {
    [self drawText:label
            inRect:NSMakeRect(rect.origin.x, rect.origin.y, rect.size.width, 18)
              size:11 weight:NSFontWeightMedium color:[NSColor colorWithWhite:0.56 alpha:1]
         alignment:NSTextAlignmentCenter];
    [self drawText:value
            inRect:NSMakeRect(rect.origin.x, rect.origin.y + 18, rect.size.width, 30)
              size:primary ? 22 : 20 weight:NSFontWeightSemibold color:color
         alignment:NSTextAlignmentCenter];
}

- (void)drawForecast:(NSDictionary *)forecast
               inRect:(NSRect)rect
              primary:(BOOL)primary
             humidity:(id)humidity {
    NSBezierPath *card = [NSBezierPath bezierPathWithRoundedRect:rect xRadius:16 yRadius:16];
    NSColor *cardColor = primary
        ? [NSColor colorWithRed:0.09 green:0.17 blue:0.25 alpha:0.98]
        : [NSColor colorWithRed:0.09 green:0.115 blue:0.16 alpha:0.96];
    [cardColor setFill];
    [card fill];
    NSColor *borderColor = primary
        ? [NSColor colorWithRed:0.32 green:0.69 blue:1.0 alpha:0.44]
        : [NSColor colorWithWhite:1.0 alpha:0.08];
    [borderColor setStroke];
    card.lineWidth = primary ? 1.5 : 1;
    [card stroke];

    NSString *description = SideboxString(forecast, @"description");
    NSString *label = SideboxString(forecast, @"date_label");
    [self drawText:label inRect:NSMakeRect(rect.origin.x + 18, rect.origin.y + 14, rect.size.width - 36, 24)
                 size:primary ? 17 : 15 weight:NSFontWeightSemibold
                color:primary ? [NSColor colorWithRed:0.61 green:0.82 blue:1 alpha:1]
                              : [NSColor colorWithWhite:0.75 alpha:1]
            alignment:NSTextAlignmentLeft];
    [self drawWeatherIcon:description
                   inRect:NSMakeRect(NSMidX(rect) - 28, rect.origin.y + 43, 56, 56)
                  primary:primary];
    [self drawMultilineText:description
                     inRect:NSMakeRect(rect.origin.x + 18, rect.origin.y + 101, rect.size.width - 36, 65)
                       size:primary ? 16 : 14 weight:NSFontWeightMedium color:[NSColor colorWithWhite:0.93 alpha:1]
                  alignment:NSTextAlignmentLeft];

    [[NSColor colorWithWhite:1 alpha:0.09] setStroke];
    NSBezierPath *metricDivider = [NSBezierPath bezierPath];
    [metricDivider moveToPoint:NSMakePoint(rect.origin.x + 16, rect.origin.y + 174)];
    [metricDivider lineToPoint:NSMakePoint(NSMaxX(rect) - 16, rect.origin.y + 174)];
    [metricDivider stroke];

    NSUInteger metricCount = primary ? 4 : 3;
    CGFloat metricsLeft = rect.origin.x + 10;
    CGFloat metricWidth = (rect.size.width - 20) / metricCount;
    CGFloat metricY = rect.origin.y + 185;
    [self drawMetric:@"最高"
               value:[self degreeValue:forecast[@"temperature_max"]]
              inRect:NSMakeRect(metricsLeft, metricY, metricWidth, 50)
               color:[NSColor colorWithRed:1.0 green:0.52 blue:0.38 alpha:1]
             primary:primary];
    [self drawMetric:@"最低"
               value:[self degreeValue:forecast[@"temperature_min"]]
              inRect:NSMakeRect(metricsLeft + metricWidth, metricY, metricWidth, 50)
               color:[NSColor colorWithRed:0.40 green:0.72 blue:1.0 alpha:1]
             primary:primary];
    [self drawMetric:@"降水"
               value:[self percentValue:forecast[@"precipitation_probability"]]
              inRect:NSMakeRect(metricsLeft + metricWidth * 2, metricY, metricWidth, 50)
               color:[NSColor colorWithRed:0.34 green:0.80 blue:1.0 alpha:1]
             primary:primary];
    if (primary) {
        [self drawMetric:@"湿度"
                   value:[self percentValue:humidity]
                  inRect:NSMakeRect(metricsLeft + metricWidth * 3, metricY, metricWidth, 50)
                   color:[NSColor colorWithRed:0.43 green:0.88 blue:0.76 alpha:1]
                 primary:YES];
    }

    NSString *wind = SideboxString(forecast, @"wind");
    if (wind.length > 0) {
        [self drawText:[@"風  " stringByAppendingString:wind]
                inRect:NSMakeRect(rect.origin.x + 18, NSMaxY(rect) - 32, rect.size.width - 36, 18)
                  size:11 weight:NSFontWeightRegular color:[NSColor colorWithWhite:0.48 alpha:1]
             alignment:NSTextAlignmentCenter];
    }
}

- (void)drawRect:(NSRect)dirtyRect {
    (void)dirtyRect;
    NSRect bounds = self.bounds;
    NSBezierPath *background = [NSBezierPath bezierPathWithRoundedRect:NSInsetRect(bounds, 1, 1)
                                                               xRadius:22 yRadius:22];
    [[NSColor colorWithRed:0.055 green:0.075 blue:0.11 alpha:0.97] setFill];
    [background fill];
    [[NSColor colorWithWhite:1.0 alpha:0.12] setStroke];
    background.lineWidth = 1;
    [background stroke];

    NSDate *now = [NSDate date];
    NSDateFormatter *clockFormatter = [[NSDateFormatter alloc] init];
    clockFormatter.locale = [[NSLocale alloc] initWithLocaleIdentifier:@"ja_JP"];
    clockFormatter.dateFormat = @"HH:mm:ss";
    NSDateFormatter *dateFormatter = [[NSDateFormatter alloc] init];
    dateFormatter.locale = clockFormatter.locale;
    dateFormatter.dateFormat = @"M月d日 EEEE";

    CGFloat width = NSWidth(bounds);
    [self drawText:[NSString stringWithFormat:@"Sidebox %@", self.version]
            inRect:NSMakeRect(34, 7, 220, 15)
              size:10 weight:NSFontWeightMedium color:[NSColor colorWithWhite:0.48 alpha:1]
         alignment:NSTextAlignmentLeft];
    [self drawClock:[clockFormatter stringFromDate:now]
             inRect:NSMakeRect(30, 24, 270, 68)];
    [self drawText:[dateFormatter stringFromDate:now]
            inRect:NSMakeRect(34, 90, 260, 27)
              size:17 weight:NSFontWeightMedium color:[NSColor colorWithWhite:0.68 alpha:1]
         alignment:NSTextAlignmentLeft];

    NSArray *daily = self.weather[@"daily"];
    CGFloat weatherX = 350;
    [self drawText:SideboxString(self.weather, @"location")
            inRect:NSMakeRect(weatherX, 32, width - weatherX - 52, 28)
              size:21 weight:NSFontWeightSemibold color:NSColor.whiteColor
         alignment:NSTextAlignmentLeft];
    [self drawText:@"気象庁  3日間予報"
            inRect:NSMakeRect(weatherX, 65, width - weatherX - 52, 21)
              size:13 weight:NSFontWeightMedium color:[NSColor colorWithWhite:0.52 alpha:1]
         alignment:NSTextAlignmentLeft];
    if (self.loadingWeather && self.weather.count > 0) {
        [self drawText:@"更新中…"
                inRect:NSMakeRect(weatherX, 88, width - weatherX - 52, 19)
                  size:12 weight:NSFontWeightRegular color:[NSColor colorWithRed:0.45 green:0.74 blue:1 alpha:1]
             alignment:NSTextAlignmentLeft];
    }

    NSString *status = self.weatherError;
    if (status.length == 0 && self.loadingWeather && self.weather.count == 0) {
        status = @"天気を取得しています…";
    }
    if (status.length > 0) {
        [self drawMultilineText:status
                         inRect:NSMakeRect(weatherX, 88, width - weatherX - 52, 31)
                           size:14 weight:NSFontWeightRegular color:[NSColor colorWithRed:1 green:0.55 blue:0.55 alpha:1]
                      alignment:NSTextAlignmentLeft];
    }

    if (![daily isKindOfClass:[NSArray class]] || daily.count == 0) {
        return;
    }
    CGFloat gap = 12;
    CGFloat left = 24;
    CGFloat cardsTop = 127;
    CGFloat contentWidth = width - left * 2;
    CGFloat usableWidth = contentWidth - gap * 2;
    CGFloat todayWidth = floor(usableWidth * 0.42);
    CGFloat futureWidth = (usableWidth - todayWidth) / 2.0;
    CGFloat cardHeight = MAX(235, NSHeight(bounds) - cardsTop - 24);
    NSUInteger count = MIN((NSUInteger)3, daily.count);
    for (NSUInteger index = 0; index < count; index++) {
        id forecast = daily[index];
        if ([forecast isKindOfClass:[NSDictionary class]]) {
            CGFloat cardX = left;
            CGFloat cardWidth = todayWidth;
            if (index > 0) {
                cardWidth = futureWidth;
                cardX = left + todayWidth + gap + (index - 1) * (futureWidth + gap);
            }
            [self drawForecast:forecast
                        inRect:NSMakeRect(cardX, cardsTop, cardWidth, cardHeight)
                       primary:index == 0
                      humidity:index == 0 ? self.weather[@"humidity"] : nil];
        }
    }
}

@end

@implementation SideboxAppDelegate

- (instancetype)initWithConfig:(NSDictionary *)config
                     configPath:(NSString *)configPath
                        version:(NSString *)version {
    self = [super init];
    if (self != nil) {
        _initialConfig = [config copy];
        _configPath = [configPath copy];
        _version = [version copy];
    }
    return self;
}

- (NSRect)windowFrameForConfig:(NSDictionary *)config {
    NSScreen *screen = NSScreen.mainScreen ?: NSScreen.screens.firstObject;
    NSRect visible = screen.visibleFrame;
    CGFloat width = MAX(680, SideboxNumber(config, @"window_width", 760));
    CGFloat height = MAX(380, SideboxNumber(config, @"window_height", 425));
    CGFloat x = NSMinX(visible) + SideboxNumber(config, @"window_x", 32);
    CGFloat y = NSMaxY(visible) - SideboxNumber(config, @"window_y", 32) - height;
    return NSMakeRect(x, y, width, height);
}

- (void)applicationDidFinishLaunching:(NSNotification *)notification {
    (void)notification;
    NSRect frame = [self windowFrameForConfig:self.initialConfig];
    NSWindowStyleMask style = NSWindowStyleMaskTitled |
                              NSWindowStyleMaskResizable |
                              NSWindowStyleMaskFullSizeContentView;
    self.window = [[NSWindow alloc] initWithContentRect:frame
                                              styleMask:style
                                                backing:NSBackingStoreBuffered
                                                  defer:NO];
    self.window.title = [NSString stringWithFormat:@"Sidebox %@", self.version];
    self.window.titleVisibility = NSWindowTitleHidden;
    self.window.titlebarAppearsTransparent = YES;
    self.window.opaque = NO;
    self.window.backgroundColor = NSColor.clearColor;
    self.window.hasShadow = YES;
    self.window.movableByWindowBackground = YES;
    self.window.minSize = NSMakeSize(680, 380);
    self.window.collectionBehavior = NSWindowCollectionBehaviorCanJoinAllSpaces |
                                     NSWindowCollectionBehaviorFullScreenAuxiliary;
    self.window.alphaValue = MIN(1.0, MAX(0.35, SideboxNumber(self.initialConfig, @"opacity", 0.94)));
    self.window.level = [self.initialConfig[@"always_on_top"] boolValue]
        ? NSFloatingWindowLevel : NSNormalWindowLevel;
    self.window.delegate = self;

    for (NSButton *button in @[
        [self.window standardWindowButton:NSWindowCloseButton],
        [self.window standardWindowButton:NSWindowMiniaturizeButton],
        [self.window standardWindowButton:NSWindowZoomButton],
    ]) {
        button.hidden = YES;
    }

    self.sideboxView = [[SideboxView alloc] initWithFrame:NSMakeRect(0, 0, frame.size.width, frame.size.height)
                                                   config:self.initialConfig
                                               configPath:self.configPath
                                                  version:self.version];
    self.sideboxView.autoresizingMask = NSViewWidthSizable | NSViewHeightSizable;
    self.window.contentView = self.sideboxView;

    NSButton *closeButton = [NSButton buttonWithTitle:@"×" target:NSApp action:@selector(terminate:)];
    closeButton.bordered = NO;
    closeButton.font = [NSFont systemFontOfSize:21 weight:NSFontWeightLight];
    closeButton.contentTintColor = [NSColor colorWithWhite:0.72 alpha:1];
    closeButton.frame = NSMakeRect(frame.size.width - 42, 8, 32, 30);
    closeButton.autoresizingMask = NSViewMinXMargin | NSViewMaxYMargin;
    [self.sideboxView addSubview:closeButton];

    [self.window makeKeyAndOrderFront:nil];
    [NSApp activateIgnoringOtherApps:YES];
    [self.sideboxView start];
    [self.sideboxView syncStartAtLoginFromConfig];
}

- (void)scheduleFrameSave {
    [NSObject cancelPreviousPerformRequestsWithTarget:self selector:@selector(saveWindowFrame) object:nil];
    [self performSelector:@selector(saveWindowFrame) withObject:nil afterDelay:0.35];
}

- (void)saveWindowFrame {
    NSScreen *screen = self.window.screen ?: NSScreen.mainScreen;
    NSRect visible = screen.visibleFrame;
    NSRect frame = self.window.frame;
    int x = (int)llround(NSMinX(frame) - NSMinX(visible));
    int y = (int)llround(NSMaxY(visible) - NSMaxY(frame));
    SideboxSaveWindowFrame(x, y, (int)llround(NSWidth(frame)), (int)llround(NSHeight(frame)));
}

- (void)windowDidMove:(NSNotification *)notification {
    (void)notification;
    [self scheduleFrameSave];
}

- (void)windowDidResize:(NSNotification *)notification {
    (void)notification;
    [self scheduleFrameSave];
}

- (BOOL)windowShouldClose:(NSWindow *)sender {
    (void)sender;
    [NSApp terminate:nil];
    return YES;
}

- (void)applicationWillTerminate:(NSNotification *)notification {
    (void)notification;
    [NSObject cancelPreviousPerformRequestsWithTarget:self selector:@selector(saveWindowFrame) object:nil];
    [self saveWindowFrame];
}

@end

static SideboxAppDelegate *sideboxDelegate;

void SideboxRun(const char *config_json, const char *config_path, const char *version) {
    @autoreleasepool {
        NSApplication *application = [NSApplication sharedApplication];
        [application setActivationPolicy:NSApplicationActivationPolicyAccessory];
        NSDictionary *config = SideboxParseJSON(config_json);
        NSString *path = config_path == NULL ? @"" : [NSString stringWithUTF8String:config_path];
        NSString *versionString = version == NULL ? @"" : [NSString stringWithUTF8String:version];
        sideboxDelegate = [[SideboxAppDelegate alloc] initWithConfig:config
                                                         configPath:path
                                                            version:versionString];
        application.delegate = sideboxDelegate;
        [application run];
    }
}

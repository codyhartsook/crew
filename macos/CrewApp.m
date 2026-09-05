#import <Cocoa/Cocoa.h>
#import <WebKit/WebKit.h>

@interface CrewDelegate : NSObject <NSApplicationDelegate>
@property NSWindow *window;
@property WKWebView *webView;
@property NSURL *pendingDashboard;
@end

@implementation CrewDelegate

- (void)applicationDidFinishLaunching:(NSNotification *)notification {
    self.webView = [[WKWebView alloc] init];
    self.window = [[NSWindow alloc]
        initWithContentRect:NSMakeRect(0, 0, 1200, 800)
        styleMask:NSWindowStyleMaskTitled | NSWindowStyleMaskClosable |
                  NSWindowStyleMaskMiniaturizable | NSWindowStyleMaskResizable
        backing:NSBackingStoreBuffered
        defer:NO];
    self.window.title = @"Crew";
    self.window.contentView = self.webView;
    [self.window center];
    [self.window makeKeyAndOrderFront:nil];
    [self load:self.pendingDashboard ?: [NSURL URLWithString:@"http://127.0.0.1:8790"]];
    [NSApp activateIgnoringOtherApps:YES];
}

- (void)application:(NSApplication *)application openURLs:(NSArray<NSURL *> *)urls {
    for (NSURL *incoming in urls) {
        NSURL *dashboard = [self dashboardURL:incoming];
        if (!dashboard) continue;
        if (self.webView) [self load:dashboard];
        else self.pendingDashboard = dashboard;
        break;
    }
}

- (BOOL)applicationShouldTerminateAfterLastWindowClosed:(NSApplication *)sender {
    return YES;
}

- (void)load:(NSURL *)url {
    [self.webView loadRequest:[NSURLRequest requestWithURL:url
        cachePolicy:NSURLRequestReloadIgnoringLocalCacheData timeoutInterval:30]];
}

- (NSURL *)dashboardURL:(NSURL *)incoming {
    if (![incoming.scheme isEqualToString:@"crew"] || ![incoming.host isEqualToString:@"dashboard"]) return nil;
    NSURLComponents *components = [NSURLComponents componentsWithURL:incoming resolvingAgainstBaseURL:NO];
    NSString *value;
    for (NSURLQueryItem *item in components.queryItems) {
        if ([item.name isEqualToString:@"url"]) { value = item.value; break; }
    }
    NSURL *url = value ? [NSURL URLWithString:value] : nil;
    NSSet *hosts = [NSSet setWithArray:@[@"localhost", @"127.0.0.1", @"::1", @"[::1]"]];
    return [url.scheme isEqualToString:@"http"] && [hosts containsObject:url.host] ? url : nil;
}

@end

int main(void) {
    @autoreleasepool {
        NSApplication *application = NSApplication.sharedApplication;
        CrewDelegate *delegate = [[CrewDelegate alloc] init];
        application.delegate = delegate;
        [application setActivationPolicy:NSApplicationActivationPolicyRegular];
        [application run];
    }
    return 0;
}

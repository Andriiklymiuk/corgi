"use strict";(self.webpackChunkcorgi_docs=self.webpackChunkcorgi_docs||[]).push([[8355],{9613:(e,r,t)=>{t.d(r,{Zo:()=>p,kt:()=>f});var o=t(9496);function n(e,r,t){return r in e?Object.defineProperty(e,r,{value:t,enumerable:!0,configurable:!0,writable:!0}):e[r]=t,e}function i(e,r){var t=Object.keys(e);if(Object.getOwnPropertySymbols){var o=Object.getOwnPropertySymbols(e);r&&(o=o.filter((function(r){return Object.getOwnPropertyDescriptor(e,r).enumerable}))),t.push.apply(t,o)}return t}function a(e){for(var r=1;r<arguments.length;r++){var t=null!=arguments[r]?arguments[r]:{};r%2?i(Object(t),!0).forEach((function(r){n(e,r,t[r])})):Object.getOwnPropertyDescriptors?Object.defineProperties(e,Object.getOwnPropertyDescriptors(t)):i(Object(t)).forEach((function(r){Object.defineProperty(e,r,Object.getOwnPropertyDescriptor(t,r))}))}return e}function c(e,r){if(null==e)return{};var t,o,n=function(e,r){if(null==e)return{};var t,o,n={},i=Object.keys(e);for(o=0;o<i.length;o++)t=i[o],r.indexOf(t)>=0||(n[t]=e[t]);return n}(e,r);if(Object.getOwnPropertySymbols){var i=Object.getOwnPropertySymbols(e);for(o=0;o<i.length;o++)t=i[o],r.indexOf(t)>=0||Object.prototype.propertyIsEnumerable.call(e,t)&&(n[t]=e[t])}return n}var l=o.createContext({}),s=function(e){var r=o.useContext(l),t=r;return e&&(t="function"==typeof e?e(r):a(a({},r),e)),t},p=function(e){var r=s(e.components);return o.createElement(l.Provider,{value:r},e.children)},m="mdxType",u={inlineCode:"code",wrapper:function(e){var r=e.children;return o.createElement(o.Fragment,{},r)}},g=o.forwardRef((function(e,r){var t=e.components,n=e.mdxType,i=e.originalType,l=e.parentName,p=c(e,["components","mdxType","originalType","parentName"]),m=s(t),g=n,f=m["".concat(l,".").concat(g)]||m[g]||u[g]||i;return t?o.createElement(f,a(a({ref:r},p),{},{components:t})):o.createElement(f,a({ref:r},p))}));function f(e,r){var t=arguments,n=r&&r.mdxType;if("string"==typeof e||n){var i=t.length,a=new Array(i);a[0]=g;var c={};for(var l in r)hasOwnProperty.call(r,l)&&(c[l]=r[l]);c.originalType=e,c[m]="string"==typeof e?e:n,a[1]=c;for(var s=2;s<i;s++)a[s]=t[s];return o.createElement.apply(null,a)}return o.createElement.apply(null,t)}g.displayName="MDXCreateElement"},682:(e,r,t)=>{t.r(r),t.d(r,{assets:()=>l,contentTitle:()=>a,default:()=>u,frontMatter:()=>i,metadata:()=>c,toc:()=>s});var o=t(8957),n=(t(9496),t(9613));const i={},a="corgi",c={unversionedId:"commands/corgi",id:"commands/corgi",title:"corgi",description:"corgi",source:"@site/docs/commands/corgi.md",sourceDirName:"commands",slug:"/commands/corgi",permalink:"/corgi/docs/commands/corgi",draft:!1,tags:[],version:"current",frontMatter:{},sidebar:"tutorialSidebar",previous:{title:"Commands",permalink:"/corgi/docs/category/commands"},next:{title:"corgi clean",permalink:"/corgi/docs/commands/corgi_clean"}},l={},s=[{value:"corgi",id:"corgi-1",level:2},{value:"Synopsis",id:"synopsis",level:3},{value:"Examples",id:"examples",level:3},{value:"Options",id:"options",level:3},{value:"SEE ALSO",id:"see-also",level:3},{value:"Auto generated by spf13/cobra on 2-Sep-2024",id:"auto-generated-by-spf13cobra-on-2-sep-2024",level:6}],p={toc:s},m="wrapper";function u(e){let{components:r,...t}=e;return(0,n.kt)(m,(0,o.Z)({},p,t,{components:r,mdxType:"MDXLayout"}),(0,n.kt)("h1",{id:"corgi"},"corgi"),(0,n.kt)("h2",{id:"corgi-1"},"corgi"),(0,n.kt)("p",null,"Corgi cli magic friend"),(0,n.kt)("h3",{id:"synopsis"},"Synopsis"),(0,n.kt)("p",null,"This cli is created to make life easier.\nThe goal is to create smth flexible and robust."),(0,n.kt)("p",null,"WOOF \ud83d\udc36"),(0,n.kt)("h3",{id:"examples"},"Examples"),(0,n.kt)("pre",null,(0,n.kt)("code",{parentName:"pre"},"corgi init\n\ncorgi run\n")),(0,n.kt)("h3",{id:"options"},"Options"),(0,n.kt)("pre",null,(0,n.kt)("code",{parentName:"pre"},'      --describe                  Describe contents of corgi-compose file\n      --dockerContext string      Specify docker context to use, can be default,orbctl,colima (default "default")\n  -l, --exampleList               List examples to choose from. Click on any example to download it\n  -f, --filename string           Custom filepath for for corgi-compose\n      --fromScratch               Clean corgi_services folder before running\n  -t, --fromTemplate string       Create corgi service from template url\n      --fromTemplateName string   Create corgi service from template name and url\n  -g, --global                    Use global path to one of the services\n  -h, --help                      help for corgi\n      --privateToken string       Private token for private repositories to download files\n      --runOnce                   Run corgi once and exit\n      --silent                    Hide all welcome messages\n')),(0,n.kt)("h3",{id:"see-also"},"SEE ALSO"),(0,n.kt)("ul",null,(0,n.kt)("li",{parentName:"ul"},(0,n.kt)("a",{parentName:"li",href:"corgi_clean"},"corgi clean"),"\t - Cleans all services"),(0,n.kt)("li",{parentName:"ul"},(0,n.kt)("a",{parentName:"li",href:"corgi_create"},"corgi create"),"\t - A command to create configurations for corgi"),(0,n.kt)("li",{parentName:"ul"},(0,n.kt)("a",{parentName:"li",href:"corgi_db"},"corgi db"),"\t - Database action helpers"),(0,n.kt)("li",{parentName:"ul"},(0,n.kt)("a",{parentName:"li",href:"corgi_docs"},"corgi docs"),"\t - Do stuff with docs"),(0,n.kt)("li",{parentName:"ul"},(0,n.kt)("a",{parentName:"li",href:"corgi_doctor"},"corgi doctor"),"\t - Check required properties in corgi-compose"),(0,n.kt)("li",{parentName:"ul"},(0,n.kt)("a",{parentName:"li",href:"corgi_fork"},"corgi fork"),"\t - Fork an existing service repositories to new repos."),(0,n.kt)("li",{parentName:"ul"},(0,n.kt)("a",{parentName:"li",href:"corgi_init"},"corgi init"),"\t - Create db service"),(0,n.kt)("li",{parentName:"ul"},(0,n.kt)("a",{parentName:"li",href:"corgi_list"},"corgi list"),"\t - List all executed corgi-compose paths"),(0,n.kt)("li",{parentName:"ul"},(0,n.kt)("a",{parentName:"li",href:"corgi_pull"},"corgi pull"),"\t - Runs git pull for each service folder"),(0,n.kt)("li",{parentName:"ul"},(0,n.kt)("a",{parentName:"li",href:"corgi_run"},"corgi run"),"\t - Run all databases and services"),(0,n.kt)("li",{parentName:"ul"},(0,n.kt)("a",{parentName:"li",href:"corgi_script"},"corgi script"),"\t - Runs script on each service, if it specified"),(0,n.kt)("li",{parentName:"ul"},(0,n.kt)("a",{parentName:"li",href:"corgi_upgrade"},"corgi upgrade"),"\t - Upgrade corgi to the latest version")),(0,n.kt)("h6",{id:"auto-generated-by-spf13cobra-on-2-sep-2024"},"Auto generated by spf13/cobra on 2-Sep-2024"))}u.isMDXComponent=!0}}]);
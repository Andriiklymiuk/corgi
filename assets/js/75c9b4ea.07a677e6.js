"use strict";(self.webpackChunkcorgi_docs=self.webpackChunkcorgi_docs||[]).push([[9259],{6669:(e,r,t)=>{t.r(r),t.d(r,{assets:()=>l,contentTitle:()=>i,default:()=>d,frontMatter:()=>a,metadata:()=>c,toc:()=>p});var o=t(6425),n=(t(7953),t(8860));const a={},i="corgi upgrade",c={unversionedId:"commands/corgi_upgrade",id:"commands/corgi_upgrade",title:"corgi upgrade",description:"corgi upgrade",source:"@site/docs/commands/corgi_upgrade.md",sourceDirName:"commands",slug:"/commands/corgi_upgrade",permalink:"/corgi/docs/commands/corgi_upgrade",draft:!1,tags:[],version:"current",frontMatter:{},sidebar:"tutorialSidebar",previous:{title:"corgi script",permalink:"/corgi/docs/commands/corgi_script"},next:{title:"How to run db helpers",permalink:"/corgi/docs/db_helpers"}},l={},p=[{value:"corgi upgrade",id:"corgi-upgrade-1",level:2},{value:"Synopsis",id:"synopsis",level:3},{value:"Options",id:"options",level:3},{value:"Options inherited from parent commands",id:"options-inherited-from-parent-commands",level:3},{value:"SEE ALSO",id:"see-also",level:3},{value:"Auto generated by spf13/cobra on 2-Sep-2024",id:"auto-generated-by-spf13cobra-on-2-sep-2024",level:6}],s={toc:p},g="wrapper";function d(e){let{components:r,...t}=e;return(0,n.yg)(g,(0,o.A)({},s,t,{components:r,mdxType:"MDXLayout"}),(0,n.yg)("h1",{id:"corgi-upgrade"},"corgi upgrade"),(0,n.yg)("h2",{id:"corgi-upgrade-1"},"corgi upgrade"),(0,n.yg)("p",null,"Upgrade corgi to the latest version"),(0,n.yg)("h3",{id:"synopsis"},"Synopsis"),(0,n.yg)("p",null,"Use this command to upgrade corgi to the latest version available in Homebrew."),(0,n.yg)("pre",null,(0,n.yg)("code",{parentName:"pre"},"corgi upgrade [flags]\n")),(0,n.yg)("h3",{id:"options"},"Options"),(0,n.yg)("pre",null,(0,n.yg)("code",{parentName:"pre"},"  -h, --help   help for upgrade\n")),(0,n.yg)("h3",{id:"options-inherited-from-parent-commands"},"Options inherited from parent commands"),(0,n.yg)("pre",null,(0,n.yg)("code",{parentName:"pre"},'      --describe                  Describe contents of corgi-compose file\n      --dockerContext string      Specify docker context to use, can be default,orbctl,colima (default "default")\n  -l, --exampleList               List examples to choose from. Click on any example to download it\n  -f, --filename string           Custom filepath for for corgi-compose\n      --fromScratch               Clean corgi_services folder before running\n  -t, --fromTemplate string       Create corgi service from template url\n      --fromTemplateName string   Create corgi service from template name and url\n  -g, --global                    Use global path to one of the services\n      --privateToken string       Private token for private repositories to download files\n      --runOnce                   Run corgi once and exit\n      --silent                    Hide all welcome messages\n')),(0,n.yg)("h3",{id:"see-also"},"SEE ALSO"),(0,n.yg)("ul",null,(0,n.yg)("li",{parentName:"ul"},(0,n.yg)("a",{parentName:"li",href:"corgi"},"corgi"),"\t - Corgi cli magic friend")),(0,n.yg)("h6",{id:"auto-generated-by-spf13cobra-on-2-sep-2024"},"Auto generated by spf13/cobra on 2-Sep-2024"))}d.isMDXComponent=!0},8860:(e,r,t)=>{t.d(r,{xA:()=>s,yg:()=>m});var o=t(7953);function n(e,r,t){return r in e?Object.defineProperty(e,r,{value:t,enumerable:!0,configurable:!0,writable:!0}):e[r]=t,e}function a(e,r){var t=Object.keys(e);if(Object.getOwnPropertySymbols){var o=Object.getOwnPropertySymbols(e);r&&(o=o.filter((function(r){return Object.getOwnPropertyDescriptor(e,r).enumerable}))),t.push.apply(t,o)}return t}function i(e){for(var r=1;r<arguments.length;r++){var t=null!=arguments[r]?arguments[r]:{};r%2?a(Object(t),!0).forEach((function(r){n(e,r,t[r])})):Object.getOwnPropertyDescriptors?Object.defineProperties(e,Object.getOwnPropertyDescriptors(t)):a(Object(t)).forEach((function(r){Object.defineProperty(e,r,Object.getOwnPropertyDescriptor(t,r))}))}return e}function c(e,r){if(null==e)return{};var t,o,n=function(e,r){if(null==e)return{};var t,o,n={},a=Object.keys(e);for(o=0;o<a.length;o++)t=a[o],r.indexOf(t)>=0||(n[t]=e[t]);return n}(e,r);if(Object.getOwnPropertySymbols){var a=Object.getOwnPropertySymbols(e);for(o=0;o<a.length;o++)t=a[o],r.indexOf(t)>=0||Object.prototype.propertyIsEnumerable.call(e,t)&&(n[t]=e[t])}return n}var l=o.createContext({}),p=function(e){var r=o.useContext(l),t=r;return e&&(t="function"==typeof e?e(r):i(i({},r),e)),t},s=function(e){var r=p(e.components);return o.createElement(l.Provider,{value:r},e.children)},g="mdxType",d={inlineCode:"code",wrapper:function(e){var r=e.children;return o.createElement(o.Fragment,{},r)}},u=o.forwardRef((function(e,r){var t=e.components,n=e.mdxType,a=e.originalType,l=e.parentName,s=c(e,["components","mdxType","originalType","parentName"]),g=p(t),u=n,m=g["".concat(l,".").concat(u)]||g[u]||d[u]||a;return t?o.createElement(m,i(i({ref:r},s),{},{components:t})):o.createElement(m,i({ref:r},s))}));function m(e,r){var t=arguments,n=r&&r.mdxType;if("string"==typeof e||n){var a=t.length,i=new Array(a);i[0]=u;var c={};for(var l in r)hasOwnProperty.call(r,l)&&(c[l]=r[l]);c.originalType=e,c[g]="string"==typeof e?e:n,i[1]=c;for(var p=2;p<a;p++)i[p]=t[p];return o.createElement.apply(null,i)}return o.createElement.apply(null,t)}u.displayName="MDXCreateElement"}}]);
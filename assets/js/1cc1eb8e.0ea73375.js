"use strict";(self.webpackChunkcorgi_docs=self.webpackChunkcorgi_docs||[]).push([[8206],{9613:(e,t,r)=>{r.d(t,{Zo:()=>p,kt:()=>f});var n=r(9496);function o(e,t,r){return t in e?Object.defineProperty(e,t,{value:r,enumerable:!0,configurable:!0,writable:!0}):e[t]=r,e}function i(e,t){var r=Object.keys(e);if(Object.getOwnPropertySymbols){var n=Object.getOwnPropertySymbols(e);t&&(n=n.filter((function(t){return Object.getOwnPropertyDescriptor(e,t).enumerable}))),r.push.apply(r,n)}return r}function a(e){for(var t=1;t<arguments.length;t++){var r=null!=arguments[t]?arguments[t]:{};t%2?i(Object(r),!0).forEach((function(t){o(e,t,r[t])})):Object.getOwnPropertyDescriptors?Object.defineProperties(e,Object.getOwnPropertyDescriptors(r)):i(Object(r)).forEach((function(t){Object.defineProperty(e,t,Object.getOwnPropertyDescriptor(r,t))}))}return e}function c(e,t){if(null==e)return{};var r,n,o=function(e,t){if(null==e)return{};var r,n,o={},i=Object.keys(e);for(n=0;n<i.length;n++)r=i[n],t.indexOf(r)>=0||(o[r]=e[r]);return o}(e,t);if(Object.getOwnPropertySymbols){var i=Object.getOwnPropertySymbols(e);for(n=0;n<i.length;n++)r=i[n],t.indexOf(r)>=0||Object.prototype.propertyIsEnumerable.call(e,r)&&(o[r]=e[r])}return o}var l=n.createContext({}),s=function(e){var t=n.useContext(l),r=t;return e&&(r="function"==typeof e?e(t):a(a({},t),e)),r},p=function(e){var t=s(e.components);return n.createElement(l.Provider,{value:t},e.children)},m="mdxType",d={inlineCode:"code",wrapper:function(e){var t=e.children;return n.createElement(n.Fragment,{},t)}},u=n.forwardRef((function(e,t){var r=e.components,o=e.mdxType,i=e.originalType,l=e.parentName,p=c(e,["components","mdxType","originalType","parentName"]),m=s(r),u=o,f=m["".concat(l,".").concat(u)]||m[u]||d[u]||i;return r?n.createElement(f,a(a({ref:t},p),{},{components:r})):n.createElement(f,a({ref:t},p))}));function f(e,t){var r=arguments,o=t&&t.mdxType;if("string"==typeof e||o){var i=r.length,a=new Array(i);a[0]=u;var c={};for(var l in t)hasOwnProperty.call(t,l)&&(c[l]=t[l]);c.originalType=e,c[m]="string"==typeof e?e:o,a[1]=c;for(var s=2;s<i;s++)a[s]=r[s];return n.createElement.apply(null,a)}return n.createElement.apply(null,r)}u.displayName="MDXCreateElement"},4640:(e,t,r)=>{r.r(t),r.d(t,{assets:()=>l,contentTitle:()=>a,default:()=>d,frontMatter:()=>i,metadata:()=>c,toc:()=>s});var n=r(8957),o=(r(9496),r(9613));const i={},a="corgi list",c={unversionedId:"commands/corgi_list",id:"commands/corgi_list",title:"corgi list",description:"corgi list",source:"@site/docs/commands/corgi_list.md",sourceDirName:"commands",slug:"/commands/corgi_list",permalink:"/corgi/docs/commands/corgi_list",draft:!1,tags:[],version:"current",frontMatter:{},sidebar:"tutorialSidebar",previous:{title:"corgi init",permalink:"/corgi/docs/commands/corgi_init"},next:{title:"corgi pull",permalink:"/corgi/docs/commands/corgi_pull"}},l={},s=[{value:"corgi list",id:"corgi-list-1",level:2},{value:"Synopsis",id:"synopsis",level:3},{value:"Options",id:"options",level:3},{value:"Options inherited from parent commands",id:"options-inherited-from-parent-commands",level:3},{value:"SEE ALSO",id:"see-also",level:3},{value:"Auto generated by spf13/cobra on 18-May-2024",id:"auto-generated-by-spf13cobra-on-18-may-2024",level:6}],p={toc:s},m="wrapper";function d(e){let{components:t,...r}=e;return(0,o.kt)(m,(0,n.Z)({},p,r,{components:t,mdxType:"MDXLayout"}),(0,o.kt)("h1",{id:"corgi-list"},"corgi list"),(0,o.kt)("h2",{id:"corgi-list-1"},"corgi list"),(0,o.kt)("p",null,"List all executed corgi-compose paths"),(0,o.kt)("h3",{id:"synopsis"},"Synopsis"),(0,o.kt)("p",null,"This command lists all the paths to corgi-compose files that have been executed."),(0,o.kt)("pre",null,(0,o.kt)("code",{parentName:"pre"},"corgi list [flags]\n")),(0,o.kt)("h3",{id:"options"},"Options"),(0,o.kt)("pre",null,(0,o.kt)("code",{parentName:"pre"},"      --cleanList   Clear all the listed paths\n  -h, --help        help for list\n")),(0,o.kt)("h3",{id:"options-inherited-from-parent-commands"},"Options inherited from parent commands"),(0,o.kt)("pre",null,(0,o.kt)("code",{parentName:"pre"},'      --describe               Describe contents of corgi-compose file\n      --dockerContext string   Specify docker context to use, can be default,orbctl,colima (default "default")\n  -f, --filename string        Custom filepath for for corgi-compose\n      --fromScratch            Clean corgi_services folder before running\n  -t, --fromTemplate string    Create corgi service from template url\n  -g, --global                 Use global path to one of the services\n      --privateToken string    Private token for private repositories to download files\n      --runOnce                Run corgi once and exit\n      --silent                 Hide all welcome messages\n')),(0,o.kt)("h3",{id:"see-also"},"SEE ALSO"),(0,o.kt)("ul",null,(0,o.kt)("li",{parentName:"ul"},(0,o.kt)("a",{parentName:"li",href:"corgi"},"corgi"),"\t - Corgi cli magic friend")),(0,o.kt)("h6",{id:"auto-generated-by-spf13cobra-on-18-may-2024"},"Auto generated by spf13/cobra on 18-May-2024"))}d.isMDXComponent=!0}}]);
"use strict";(self.webpackChunkcorgi_docs=self.webpackChunkcorgi_docs||[]).push([[4631],{9613:(e,r,n)=>{n.d(r,{Zo:()=>p,kt:()=>f});var t=n(9496);function o(e,r,n){return r in e?Object.defineProperty(e,r,{value:n,enumerable:!0,configurable:!0,writable:!0}):e[r]=n,e}function c(e,r){var n=Object.keys(e);if(Object.getOwnPropertySymbols){var t=Object.getOwnPropertySymbols(e);r&&(t=t.filter((function(r){return Object.getOwnPropertyDescriptor(e,r).enumerable}))),n.push.apply(n,t)}return n}function i(e){for(var r=1;r<arguments.length;r++){var n=null!=arguments[r]?arguments[r]:{};r%2?c(Object(n),!0).forEach((function(r){o(e,r,n[r])})):Object.getOwnPropertyDescriptors?Object.defineProperties(e,Object.getOwnPropertyDescriptors(n)):c(Object(n)).forEach((function(r){Object.defineProperty(e,r,Object.getOwnPropertyDescriptor(n,r))}))}return e}function a(e,r){if(null==e)return{};var n,t,o=function(e,r){if(null==e)return{};var n,t,o={},c=Object.keys(e);for(t=0;t<c.length;t++)n=c[t],r.indexOf(n)>=0||(o[n]=e[n]);return o}(e,r);if(Object.getOwnPropertySymbols){var c=Object.getOwnPropertySymbols(e);for(t=0;t<c.length;t++)n=c[t],r.indexOf(n)>=0||Object.prototype.propertyIsEnumerable.call(e,n)&&(o[n]=e[n])}return o}var l=t.createContext({}),s=function(e){var r=t.useContext(l),n=r;return e&&(n="function"==typeof e?e(r):i(i({},r),e)),n},p=function(e){var r=s(e.components);return t.createElement(l.Provider,{value:r},e.children)},d="mdxType",m={inlineCode:"code",wrapper:function(e){var r=e.children;return t.createElement(t.Fragment,{},r)}},u=t.forwardRef((function(e,r){var n=e.components,o=e.mdxType,c=e.originalType,l=e.parentName,p=a(e,["components","mdxType","originalType","parentName"]),d=s(n),u=o,f=d["".concat(l,".").concat(u)]||d[u]||m[u]||c;return n?t.createElement(f,i(i({ref:r},p),{},{components:n})):t.createElement(f,i({ref:r},p))}));function f(e,r){var n=arguments,o=r&&r.mdxType;if("string"==typeof e||o){var c=n.length,i=new Array(c);i[0]=u;var a={};for(var l in r)hasOwnProperty.call(r,l)&&(a[l]=r[l]);a.originalType=e,a[d]="string"==typeof e?e:o,i[1]=a;for(var s=2;s<c;s++)i[s]=n[s];return t.createElement.apply(null,i)}return t.createElement.apply(null,n)}u.displayName="MDXCreateElement"},5083:(e,r,n)=>{n.r(r),n.d(r,{assets:()=>l,contentTitle:()=>i,default:()=>m,frontMatter:()=>c,metadata:()=>a,toc:()=>s});var t=n(8957),o=(n(9496),n(9613));const c={},i="corgi clean",a={unversionedId:"commands/corgi_clean",id:"commands/corgi_clean",title:"corgi clean",description:"corgi clean",source:"@site/docs/commands/corgi_clean.md",sourceDirName:"commands",slug:"/commands/corgi_clean",permalink:"/corgi/docs/commands/corgi_clean",draft:!1,tags:[],version:"current",frontMatter:{},sidebar:"tutorialSidebar",previous:{title:"corgi",permalink:"/corgi/docs/commands/corgi"},next:{title:"corgi create",permalink:"/corgi/docs/commands/corgi_create"}},l={},s=[{value:"corgi clean",id:"corgi-clean-1",level:2},{value:"Synopsis",id:"synopsis",level:3},{value:"Examples",id:"examples",level:3},{value:"Options",id:"options",level:3},{value:"Options inherited from parent commands",id:"options-inherited-from-parent-commands",level:3},{value:"SEE ALSO",id:"see-also",level:3},{value:"Auto generated by spf13/cobra on 18-Sep-2023",id:"auto-generated-by-spf13cobra-on-18-sep-2023",level:6}],p={toc:s},d="wrapper";function m(e){let{components:r,...n}=e;return(0,o.kt)(d,(0,t.Z)({},p,n,{components:r,mdxType:"MDXLayout"}),(0,o.kt)("h1",{id:"corgi-clean"},"corgi clean"),(0,o.kt)("h2",{id:"corgi-clean-1"},"corgi clean"),(0,o.kt)("p",null,"Cleans all services"),(0,o.kt)("h3",{id:"synopsis"},"Synopsis"),(0,o.kt)("p",null,"Cleans all db, corgi_services folder, cloned repos, etc.\nUseful to clean start corgi as new.\nSimilar to --fromScratch flag used in other commands, but this is more generic."),(0,o.kt)("p",null,"Requires items flag."),(0,o.kt)("pre",null,(0,o.kt)("code",{parentName:"pre"},"corgi clean [flags]\n")),(0,o.kt)("h3",{id:"examples"},"Examples"),(0,o.kt)("pre",null,(0,o.kt)("code",{parentName:"pre"},"corgi clean -i all\ncorgi clean -i db,corgi_services,services\ncorgi clean -i db\n")),(0,o.kt)("h3",{id:"options"},"Options"),(0,o.kt)("pre",null,(0,o.kt)("code",{parentName:"pre"},"  -h, --help            help for clean\n  -i, --items strings   Slice of items to clean, like: db,corgi_services,services. \n                                \n                        db - down all databases, that were added to corgi_services folder.\n                        corgi_services - clean corgi_services folder.\n                        services - delete all services folders (useful, when you want to clean cloned repos folders)\n                        \n                        all - equal to writing db,corgi_services,services in items\n                                \n")),(0,o.kt)("h3",{id:"options-inherited-from-parent-commands"},"Options inherited from parent commands"),(0,o.kt)("pre",null,(0,o.kt)("code",{parentName:"pre"},"      --describe          Describe contents of corgi-compose file\n  -f, --filename string   Custom filepath for for corgi-compose\n      --fromScratch       Clean corgi_services folder before running\n      --silent            Hide all welcome messages\n")),(0,o.kt)("h3",{id:"see-also"},"SEE ALSO"),(0,o.kt)("ul",null,(0,o.kt)("li",{parentName:"ul"},(0,o.kt)("a",{parentName:"li",href:"corgi"},"corgi"),"\t - Corgi cli magic friend")),(0,o.kt)("h6",{id:"auto-generated-by-spf13cobra-on-18-sep-2023"},"Auto generated by spf13/cobra on 18-Sep-2023"))}m.isMDXComponent=!0}}]);
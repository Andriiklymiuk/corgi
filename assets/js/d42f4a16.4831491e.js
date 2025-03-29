"use strict";(self.webpackChunkcorgi_docs=self.webpackChunkcorgi_docs||[]).push([[2599],{1738:(e,r,o)=>{o.r(r),o.d(r,{assets:()=>l,contentTitle:()=>c,default:()=>m,frontMatter:()=>i,metadata:()=>a,toc:()=>s});var t=o(6425),n=(o(7953),o(8860));const i={},c="corgi doctor",a={unversionedId:"commands/corgi_doctor",id:"commands/corgi_doctor",title:"corgi doctor",description:"corgi doctor",source:"@site/docs/commands/corgi_doctor.md",sourceDirName:"commands",slug:"/commands/corgi_doctor",permalink:"/corgi/docs/commands/corgi_doctor",draft:!1,tags:[],version:"current",frontMatter:{},sidebar:"tutorialSidebar",previous:{title:"corgi docs",permalink:"/corgi/docs/commands/corgi_docs"},next:{title:"corgi fork",permalink:"/corgi/docs/commands/corgi_fork"}},l={},s=[{value:"corgi doctor",id:"corgi-doctor-1",level:2},{value:"Synopsis",id:"synopsis",level:3},{value:"Options",id:"options",level:3},{value:"Options inherited from parent commands",id:"options-inherited-from-parent-commands",level:3},{value:"SEE ALSO",id:"see-also",level:3},{value:"Auto generated by spf13/cobra on 2-Sep-2024",id:"auto-generated-by-spf13cobra-on-2-sep-2024",level:6}],p={toc:s},d="wrapper";function m(e){let{components:r,...o}=e;return(0,n.yg)(d,(0,t.A)({},p,o,{components:r,mdxType:"MDXLayout"}),(0,n.yg)("h1",{id:"corgi-doctor"},"corgi doctor"),(0,n.yg)("h2",{id:"corgi-doctor-1"},"corgi doctor"),(0,n.yg)("p",null,"Check required properties in corgi-compose"),(0,n.yg)("h3",{id:"synopsis"},"Synopsis"),(0,n.yg)("p",null,"Checks what is required for corgi-compose and installs, if not found."),(0,n.yg)("pre",null,(0,n.yg)("code",{parentName:"pre"},"corgi doctor [flags]\n")),(0,n.yg)("h3",{id:"options"},"Options"),(0,n.yg)("pre",null,(0,n.yg)("code",{parentName:"pre"},"  -h, --help   help for doctor\n")),(0,n.yg)("h3",{id:"options-inherited-from-parent-commands"},"Options inherited from parent commands"),(0,n.yg)("pre",null,(0,n.yg)("code",{parentName:"pre"},'      --describe                  Describe contents of corgi-compose file\n      --dockerContext string      Specify docker context to use, can be default,orbctl,colima (default "default")\n  -l, --exampleList               List examples to choose from. Click on any example to download it\n  -f, --filename string           Custom filepath for for corgi-compose\n      --fromScratch               Clean corgi_services folder before running\n  -t, --fromTemplate string       Create corgi service from template url\n      --fromTemplateName string   Create corgi service from template name and url\n  -g, --global                    Use global path to one of the services\n      --privateToken string       Private token for private repositories to download files\n      --runOnce                   Run corgi once and exit\n      --silent                    Hide all welcome messages\n')),(0,n.yg)("h3",{id:"see-also"},"SEE ALSO"),(0,n.yg)("ul",null,(0,n.yg)("li",{parentName:"ul"},(0,n.yg)("a",{parentName:"li",href:"corgi"},"corgi"),"\t - Corgi cli magic friend")),(0,n.yg)("h6",{id:"auto-generated-by-spf13cobra-on-2-sep-2024"},"Auto generated by spf13/cobra on 2-Sep-2024"))}m.isMDXComponent=!0},8860:(e,r,o)=>{o.d(r,{xA:()=>p,yg:()=>u});var t=o(7953);function n(e,r,o){return r in e?Object.defineProperty(e,r,{value:o,enumerable:!0,configurable:!0,writable:!0}):e[r]=o,e}function i(e,r){var o=Object.keys(e);if(Object.getOwnPropertySymbols){var t=Object.getOwnPropertySymbols(e);r&&(t=t.filter((function(r){return Object.getOwnPropertyDescriptor(e,r).enumerable}))),o.push.apply(o,t)}return o}function c(e){for(var r=1;r<arguments.length;r++){var o=null!=arguments[r]?arguments[r]:{};r%2?i(Object(o),!0).forEach((function(r){n(e,r,o[r])})):Object.getOwnPropertyDescriptors?Object.defineProperties(e,Object.getOwnPropertyDescriptors(o)):i(Object(o)).forEach((function(r){Object.defineProperty(e,r,Object.getOwnPropertyDescriptor(o,r))}))}return e}function a(e,r){if(null==e)return{};var o,t,n=function(e,r){if(null==e)return{};var o,t,n={},i=Object.keys(e);for(t=0;t<i.length;t++)o=i[t],r.indexOf(o)>=0||(n[o]=e[o]);return n}(e,r);if(Object.getOwnPropertySymbols){var i=Object.getOwnPropertySymbols(e);for(t=0;t<i.length;t++)o=i[t],r.indexOf(o)>=0||Object.prototype.propertyIsEnumerable.call(e,o)&&(n[o]=e[o])}return n}var l=t.createContext({}),s=function(e){var r=t.useContext(l),o=r;return e&&(o="function"==typeof e?e(r):c(c({},r),e)),o},p=function(e){var r=s(e.components);return t.createElement(l.Provider,{value:r},e.children)},d="mdxType",m={inlineCode:"code",wrapper:function(e){var r=e.children;return t.createElement(t.Fragment,{},r)}},g=t.forwardRef((function(e,r){var o=e.components,n=e.mdxType,i=e.originalType,l=e.parentName,p=a(e,["components","mdxType","originalType","parentName"]),d=s(o),g=n,u=d["".concat(l,".").concat(g)]||d[g]||m[g]||i;return o?t.createElement(u,c(c({ref:r},p),{},{components:o})):t.createElement(u,c({ref:r},p))}));function u(e,r){var o=arguments,n=r&&r.mdxType;if("string"==typeof e||n){var i=o.length,c=new Array(i);c[0]=g;var a={};for(var l in r)hasOwnProperty.call(r,l)&&(a[l]=r[l]);a.originalType=e,a[d]="string"==typeof e?e:n,c[1]=a;for(var s=2;s<i;s++)c[s]=o[s];return t.createElement.apply(null,c)}return t.createElement.apply(null,o)}g.displayName="MDXCreateElement"}}]);